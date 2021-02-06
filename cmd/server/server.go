package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/amimof/huego"
	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/bot"
	"github.com/frizinak/homechat/bound"
	"github.com/frizinak/homechat/server"
	"github.com/frizinak/homechat/server/channel"
	chatpkg "github.com/frizinak/homechat/server/channel/chat"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	"github.com/frizinak/homechat/server/channel/history"
	"github.com/frizinak/homechat/server/channel/music"
	"github.com/frizinak/homechat/server/channel/ping"
	"github.com/frizinak/homechat/server/channel/status"
	"github.com/frizinak/homechat/server/channel/typing"
	"github.com/frizinak/homechat/server/channel/update"
	"github.com/frizinak/homechat/server/channel/upload"
	"github.com/frizinak/homechat/server/channel/users"
	"github.com/frizinak/homechat/vars"
	"github.com/nightlyone/lockfile"
)

type flock struct {
	path  string
	mutex lockfile.Lockfile
}

func hue(f *Flags) error {
	var hub *huego.Bridge
	if f.AppConf.HueIP == "" {
		var err error
		fmt.Println("No ip set in config, discovering hue bridge")
		hub, err = huego.Discover()
		if err != nil {
			return fmt.Errorf("Something went wrong during discover: %w", err)
		}
		f.AppConf.HueIP = hub.Host
		fmt.Printf("found hub at '%s'\n", f.AppConf.HueIP)
		if err := f.AppConf.Encode(f.All.ConfigFile); err != nil {
			return err
		}
	}

	fmt.Printf("creating new app on bridge at ip %s\n", f.AppConf.HueIP)
	if hub == nil {
		hub = huego.New(f.AppConf.HueIP, "")
	}
	fmt.Println("go press the bridge button and press enter to continue")
	fmt.Scanln()
	pass, err := hub.CreateUser("homechat")
	if err != nil {
		return err
	}
	f.AppConf.HuePass = pass
	if err := f.AppConf.Encode(f.All.ConfigFile); err != nil {
		return err
	}

	fmt.Println("success")
	return nil
}

func fingerprint(f *Flags) error {
	pk, err := f.All.Key.Public()
	if err != nil {
		return fmt.Errorf("failed to parse publickey: %w", err)
	}

	fmt.Println(pk.FingerprintString())
	return nil
}

func logs(f *Flags) error {
	if f.Logs.Dir == "" {
		return errors.New("no directory specified")
	}
	log := f.ServerConf.Log
	chat := &chatpkg.ChatChannel{}
	hist, err := history.New(log, f.AppConf.MaxChatMessages, "", chat)
	if err != nil {
		return err
	}
	*chat = *chatpkg.New(log, hist)

	glob, err := filepath.Glob(filepath.Join(f.Logs.Dir, "*"))
	if err != nil {
		return err
	}
	sort.Strings(glob)

	cb := func(msg channel.Msg) {
		l := msg.(history.Log)
		m := l.Msg.(chatdata.Message)
		fmt.Printf("%s %-10s | %s\n", l.Stamp.Format("2006-01-02 15:04:05"), l.From.Name(), m.Data)
	}

	do := func(path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		err = hist.DecodeAppendFile(f, cb)
		if err != nil {
			return fmt.Errorf("an error occurred in '%s': %w", path, err)
		}
		return nil
	}

	for _, p := range glob {
		if err := do(p); err != nil {
			return err
		}
	}

	return nil
}

func serve(flock flock, f *Flags) error {
	fmt.Println("Claiming lock")
	for {
		if err := flock.mutex.TryLock(); err != nil {
			if err != lockfile.ErrNotExist {
				panic(fmt.Errorf("Failed to get lock: '%s': %w", flock.path, err))
			}
			fmt.Printf("could not claim lock at %s, retrying...\n", flock.path)
			time.Sleep(time.Second)
			continue
		}
		break
	}
	defer flock.mutex.Unlock()

	fmt.Println("Loading assets")
	static := make(map[string][]byte)
	sign := func(a string, data []byte) error {
		sig, err := f.All.Key.Sign(data)
		if err != nil {
			return err
		}
		enc := base64.RawURLEncoding
		n := make([]byte, enc.EncodedLen(len(sig)))
		enc.Encode(n, sig)
		static[a+".sig"] = sig
		static[a+".sig.base64"] = n
		return nil
	}

	err := func() error {
		fs := []string{
			"index.html",
			"app.wasm",
		}

		for _, f := range fs {
			static[f] = bound.MustAsset(f)
		}

		names, err := bound.AssetDir("clients")
		if err != nil {
			return err
		}
		for _, n := range names {
			a := "clients/" + n
			if strings.HasSuffix(n, ".gz") {
				continue
			}
			static[a] = bound.MustAsset(a)
			err := sign(a, static[a])
			if err != nil {
				return err
			}
		}
		for k := range static {
			g := k + ".gz"
			a, err := bound.Asset(g)
			if err == nil {
				static[g] = a
			}
		}
		return nil
	}()
	if err != nil {
		return err
	}

	func() {
		if f.Serve.HTTPDir != "" {
			return
		}
		re := regexp.MustCompile(`(?s)<!--scripts-->.*<!--eoscripts-->`)
		buf := bytes.NewBuffer(nil)

		buf.WriteString("<script>")
		buf.Write(bound.MustAsset("wasm_exec.js"))
		buf.Write(bound.MustAsset("wasm_init.js"))
		buf.Write(bound.MustAsset("app.js"))
		buf.WriteString("</script>")

		static["index.html"] = re.ReplaceAllLiteral(static["index.html"], buf.Bytes())

		re = regexp.MustCompile(`(?s)<!--style-->.*<!--eostyle-->`)
		buf.Reset()

		buf.WriteString("<style>")
		buf.Write(bound.MustAsset("style.css"))
		buf.WriteString("</style>")

		static["index.html"] = re.ReplaceAllLiteral(static["index.html"], buf.Bytes())

		buf.Reset()
		w := gzip.NewWriter(buf)
		w.Write(static["index.html"])
		w.Close()
		static["index.html.gz"] = buf.Bytes()
	}()

	var fh http.Handler
	if f.Serve.HTTPDir != "" {
		fh = http.FileServer(http.Dir(f.Serve.HTTPDir))
	}

	router := func(r *http.Request, l *log.Logger) (simplehttp.HandleFunc, int) {
		p := strings.TrimLeft(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}

		if fh != nil {
			return func(w http.ResponseWriter, r *http.Request, l *log.Logger) (int, error) {
				fh.ServeHTTP(w, r)
				return 0, nil
			}, 0
		}

		if _, ok := static[p]; ok {
			return func(w http.ResponseWriter, r *http.Request, l *log.Logger) (int, error) {
				ctype := mime.TypeByExtension(filepath.Ext(p))
				if ctype != "" {
					w.Header().Set("Content-Type", ctype)
				}
				if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
					gz := p + ".gz"
					if _, ok := static[gz]; ok {
						w.Header().Set("Content-Encoding", "gzip")
						p = gz
					}
				}

				_, err := w.Write(static[p])
				return 0, err
			}, 0
		}

		return nil, 0
	}

	c := f.ServerConf
	c.Router = router
	s, err := server.New(c)
	if err != nil {
		return err
	}

	now := time.Now().Format("2006-01-02--15-04-05.999999999")
	rnd := make([]byte, 128)
	if _, err := io.ReadFull(crand.Reader, rnd); err != nil {
		return err
	}
	hsh := fnv.New64()
	hsh.Write(rnd)

	appendChatFile := filepath.Join(
		f.Logs.Dir,
		fmt.Sprintf(
			"chat-%s-%s.log",
			now,
			base64.RawURLEncoding.EncodeToString(hsh.Sum(nil)),
		),
	)

	fmt.Println("Registering channels")
	chat := &chatpkg.ChatChannel{}
	history, err := history.New(c.Log, f.AppConf.MaxChatMessages, appendChatFile, chat)
	if err != nil {
		return err
	}
	musicErr := status.New()
	music := music.NewYM(c.Log, musicErr, f.AppConf.YMDir)
	*chat = *chatpkg.New(c.Log, history)
	upload := upload.New(c.MaxUploadSize, chat, s)
	users := users.New([]string{vars.ChatChannel, vars.MusicChannel}, s)
	typing := typing.New([]string{vars.ChatChannel})
	update := update.New(func(os, arch string) (sig []byte, data []byte, ok bool) {
		suf := ""
		if os == "windows" {
			suf = ".exe"
		}
		k := fmt.Sprintf("clients/homechat-%s-%s%s", os, arch, suf)
		s := fmt.Sprintf("%s.sig", k)
		data, ok = static[k]
		if !ok {
			return
		}
		sig, ok = static[s]
		return
	})

	s.MustAddChannel(vars.UpdateChannel, update)
	s.MustAddChannel(vars.ChatChannel, chat)
	s.MustAddChannel(vars.UploadChannel, upload)
	s.MustAddChannel(vars.HistoryChannel, history)
	s.MustAddChannel(vars.PingChannel, ping.New())
	s.MustAddChannel(vars.TypingChannel, typing)
	s.MustAddChannel(vars.UserChannel, users)
	s.MustAddChannel(vars.MusicChannel, music)
	s.MustAddChannel(vars.MusicStateChannel, music.StateChannel())
	s.MustAddChannel(vars.MusicSongChannel, music.SongChannel())
	s.MustAddChannel(vars.MusicPlaylistChannel, music.PlaylistChannel())
	s.MustAddChannel(vars.MusicErrorChannel, musicErr)
	s.MustAddChannel(vars.MusicNodeChannel, music.NodeChannel())

	s.MustSetUserUpdateHandler(channel.MultiUserUpdateHandler(users, chat))

	go music.SendInterval(time.Millisecond * 1000)
	go music.StateSendInterval(time.Millisecond * 100)
	go music.PlaylistSendInterval(time.Millisecond * 5000)
	go users.SendInterval(time.Millisecond * 500)

	fmt.Println("Birthing bots")
	quoteBots := bot.NewBotCollection("quote-bot")
	quoteBots.AddBot("programming", bot.NewBotFunc(bot.ProgrammingQuote))
	quoteBots.AddBot("cats", bot.NewBotFunc(bot.CatQuote))

	weatherBot := bot.NewWttrBot(f.AppConf.WttrCity)

	if f.AppConf.HueIP != "" {
		hue := bot.NewHueBot(
			f.AppConf.HueIP,
			f.AppConf.HuePass,
			[]string{},
		)

		chat.AddBot("hue", hue)
	}

	chat.AddBot("quote", quoteBots)
	chat.AddBot("holidays", bot.NewHolidayBot(f.AppConf.HolidayCountryCode))
	chat.AddBot("wttr", weatherBot)
	chat.AddBot("weather", weatherBot)
	chat.AddBot("trivia", bot.NewTriviaBot())

	exit := make(chan struct{}, 1)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sig
		fmt.Println("saving")
		serverErr := s.Save()
		ymErr := music.SaveCollection()
		if serverErr != nil {
			fmt.Fprintf(
				os.Stderr,
				"error occurred when trying to run server.Save: %s",
				serverErr.Error(),
			)
		}
		if ymErr != nil {
			fmt.Fprintf(
				os.Stderr,
				"error occurred when trying to run libym.Collection.Save %s",
				ymErr.Error(),
			)
		}
		exit <- struct{}{}
	}()

	fmt.Printf("Starting server on http://%s tcp://%s\n", c.HTTPAddress, c.TCPAddress)
	if err := s.Init(); err != nil {
		return err
	}

	var tcpErr, httpErr, channelErr error
	errs := make(chan struct{})

	go func() {
		if err := s.RunChannels(); err != nil {
			channelErr = err
			errs <- struct{}{}
			return
		}
	}()

	go func() {
		retries := 3
		var err error
		for retries > 0 {
			retries--
			if err = s.RunTCP(); err == nil {
				break
			}
			if retries > 0 {
				fmt.Printf("Failed to bind TCP server to %s, retrying in 5s...\n", c.TCPAddress)
			}
			time.Sleep(time.Second * 5)
		}

		if err != nil {
			tcpErr = err
			errs <- struct{}{}
			return
		}
	}()

	go func() {
		retries := 3
		var err error
		for retries > 0 {
			retries--
			if err = s.RunHTTP(); err == nil {
				break
			}
			if retries > 0 {
				fmt.Printf("Failed to bind HTTP server to %s, retrying in 5s...\n", c.HTTPAddress)
			}
			time.Sleep(time.Second * 5)
		}

		if err != nil {
			httpErr = err
			errs <- struct{}{}
			return
		}
	}()

outer:
	for {
		select {
		case <-exit:
			if err := s.Close(); err != nil {
				return err
			}
			break outer
		case <-errs:
			switch {
			case tcpErr != nil:
				fmt.Printf("TCP error: %s\n", tcpErr.Error())
				fmt.Println("Continuing without TCP")
				tcpErr = nil
			case httpErr != nil:
				fmt.Printf("HTTP error: %s\n", httpErr.Error())
				fmt.Println("Continuing without HTTP")
				httpErr = nil
			case channelErr != nil:
				if err := s.Close(); err != nil {
					return fmt.Errorf("%w\nadditionally: %s", channelErr, err)
				}
				return channelErr
			}
		}
	}

	fmt.Println("bye...")
	return nil
}

func main() {
	rand.Seed(time.Now().UnixNano())
	_confDir, err := os.UserConfigDir()
	var confFile string
	if err == nil {
		confFile = filepath.Join(_confDir, "homechat", "server.json")
	}

	ucache, err := os.UserCacheDir()
	cache := ""
	if err == nil {
		cache = filepath.Join(ucache, "homechat")
	}
	f := NewFlags(os.Stdout, confFile, cache)
	f.Flags()
	if err := f.Parse(); err != nil {
		panic(err)
	}

	mutexPath, err := filepath.Abs(filepath.Join(f.All.CacheDir, "~lock"))
	if err != nil {
		panic(err)
	}

	mutex, err := lockfile.New(mutexPath)
	if err != nil {
		panic(err)
	}

	flock := flock{mutexPath, mutex}
	switch f.All.Mode {
	case ModeDefault:
		err = serve(flock, f)
	case ModeLogs:
		err = logs(f)
	case ModeHue:
		err = hue(f)
	case ModeFingerprint:
		err = fingerprint(f)
	default:
		err = errors.New("no such mode")
	}

	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
