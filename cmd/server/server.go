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
	"io/ioutil"
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
	"github.com/frizinak/homechat/server/channel/upload"
	"github.com/frizinak/homechat/server/channel/users"
	"github.com/frizinak/homechat/vars"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"
	"github.com/nightlyone/lockfile"
)

type flock struct {
	path  string
	mutex lockfile.Lockfile
}

func musicFiles(f *Flags) error {
	di := di.New(
		di.Config{
			Log:          log.New(os.Stderr, "", 0),
			StorePath:    f.Music.Dir,
			AutoSave:     false,
			SimpleOutput: ioutil.Discard,
		},
	)
	col := di.Collection()
	var total uint64
	convert := func(b server.Bytes) server.Bytes {
		if f.MusicFiles.KiB {
			return b.Convert(server.KiB)
		}
		return b.Human()
	}

	for _, path := range col.UnreferencedDownloads() {
		if f.MusicFiles.Stat {
			s, err := os.Stat(path)
			if err != nil {
				fmt.Printf("%10s\t%s\n", "?", path)
				continue
			}
			size := s.Size()
			v := server.NewBytes(float64(size/1024), server.KiB)
			fmt.Printf("%10s\t%s\n", convert(v), path)
			total += uint64(size)
			continue
		}
		fmt.Println(path)
	}

	if f.MusicFiles.Stat {
		v := server.NewBytes(float64(total/1024), server.KiB)
		fmt.Printf("Total: %s\n", convert(v).String())
	}

	return nil
}

func musicDownloads(f *Flags) error {
	di := di.New(
		di.Config{
			Log:          log.New(os.Stderr, "", 0),
			StorePath:    f.Music.Dir,
			AutoSave:     false,
			SimpleOutput: ioutil.Discard,
		},
	)
	col := di.Collection()
	q := di.Queue()

	all := col.Songs()
	all = append(all, q.Slice()...)
	uniq := make(map[string]struct{}, len(all))
	count := 0
	for _, s := range all {
		gid := collection.GlobalID(s)
		if _, ok := uniq[gid]; ok {
			continue
		}
		uniq[gid] = struct{}{}
		if !s.Local() {
			fmt.Printf("[%s] %s\n", gid, s.Title())
			count++
		}
	}

	fmt.Printf("Not downloaded: %d\n", count)
	return nil
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
			static[a] = bound.MustAsset(a)
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
	users := users.New(
		[]string{vars.ChatChannel, vars.MusicChannel},
		s,
	)

	s.MustAddChannel(vars.ChatChannel, chat)
	s.MustAddChannel(vars.UploadChannel, upload)
	s.MustAddChannel(vars.HistoryChannel, history)
	s.MustAddChannel(vars.PingChannel, ping.New())
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	var saveErr error
	go func() {
		<-sig
		fmt.Println("saving")
		serverErr := s.Save()
		ymErr := music.SaveCollection()
		if serverErr != nil {
			saveErr = fmt.Errorf(
				"error occurred when trying to run server.Save: %w",
				serverErr,
			)
		}
		if ymErr != nil {
			saveErr = fmt.Errorf(
				"error occurred when trying to run libym.Collection.Save %w, additionally: %s",
				serverErr,
				err.Error(),
			)
		}
		if saveErr != nil {
			fmt.Fprintln(os.Stderr, saveErr)
		}
		s.Close()
	}()

	fmt.Printf("Starting server on http://%s tcp://%s\n", c.HTTPAddress, c.TCPAddress)
	if err := s.Init(); err != nil {
		return err
	}

	if err := s.Run(); err != nil {
		return err
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
	case ModeMusicFiles:
		err = musicFiles(f)
	case ModeMusicDownloads:
		err = musicDownloads(f)
	default:
		err = errors.New("no such mode")
	}

	if err != nil {
		panic(err)
	}
}
