package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/base64"
	"flag"
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/bot"
	"github.com/frizinak/homechat/bound"
	"github.com/frizinak/homechat/server"
	"github.com/frizinak/homechat/server/channel"
	chatpkg "github.com/frizinak/homechat/server/channel/chat"
	"github.com/frizinak/homechat/server/channel/history"
	"github.com/frizinak/homechat/server/channel/music"
	"github.com/frizinak/homechat/server/channel/ping"
	"github.com/frizinak/homechat/server/channel/status"
	"github.com/frizinak/homechat/server/channel/upload"
	"github.com/frizinak/homechat/server/channel/users"
	"github.com/frizinak/homechat/vars"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	_confDir, err := os.UserConfigDir()
	var confFile string
	if err == nil {
		confFile = filepath.Join(_confDir, "homechat", "server.json")
	}

	var dynamicHTTPDir string
	flag.StringVar(&confFile, "c", confFile, "config file")
	flag.StringVar(&dynamicHTTPDir, "http", "", "Directory the http server will directly serve from [for debugging]")
	flag.Parse()

	cache := filepath.Dir(confFile)
	ucache, err := os.UserCacheDir()
	if err == nil {
		cache = filepath.Join(ucache, "homechat")
	}

	appConf := &Config{}
	if err := appConf.Decode(confFile); err != nil {
		if os.IsNotExist(err) {
			if err := appConf.Encode(confFile); err != nil {
				panic(err)
			}
		} else if err != nil {
			panic(err)
		}
	}

	if appConf.Directory != "" {
		cache = appConf.Directory
	}

	addr := strings.Split(appConf.HTTPAddr, ":")
	if len(addr) != 2 {
		addr = []string{"127.0.0.1", "1200"}
	}

	port, err := strconv.Atoi(addr[1])
	if err != nil {
		panic(fmt.Errorf("Failed to parse server http address %w", err))
	}

	bandwidthIntervalSeconds := 0
	appendChatDir := filepath.Join(cache, "chatlogs")
	var maxUploadKBytes int64 = 1024 * 10
	resave := appConf.Merge(&Config{
		Directory: cache,
		HTTPAddr:  "127.0.0.1:1200",
		TCPAddr:   fmt.Sprintf("%s:%d", addr[0], port+1),
		YMDir:     filepath.Join(cache, "ym"),

		BandwidthIntervalSeconds: &bandwidthIntervalSeconds,
		MaxUploadKBytes:          &maxUploadKBytes,

		ChatMessagesAppendOnlyDir: &appendChatDir,
		MaxChatMessages:           500,

		WttrCity:           "tashkent",
		HolidayCountryCode: "UZ",
	})

	bandwidthIntervalSeconds = *appConf.BandwidthIntervalSeconds
	appendChatDir = *appConf.ChatMessagesAppendOnlyDir
	maxUploadKBytes = *appConf.MaxUploadKBytes

	if resave {
		if err := appConf.Encode(confFile); err != nil {
			panic(err)
		}
	}

	store := filepath.Join(appConf.Directory, "chat.log")
	uploads := filepath.Join(appConf.Directory, "uploads")
	os.MkdirAll(uploads, 0o700)
	if appendChatDir != "" {
		os.MkdirAll(appendChatDir, 0o700)
	}

	static := make(map[string][]byte)
	func() {
		fs := []string{
			"index.html",
			"app.wasm",
		}

		for _, f := range fs {
			static[f] = bound.MustAsset(f)
		}

		names, err := bound.AssetDir("clients")
		if err != nil {
			panic(err)
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
	}()

	func() {
		if dynamicHTTPDir != "" {
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
	if dynamicHTTPDir != "" {
		fh = http.FileServer(http.Dir(dynamicHTTPDir))
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

	c := server.Config{
		ProtocolVersion: vars.ProtocolVersion,
		Log:             log.New(os.Stderr, "", 0),
		HTTPAddress:     appConf.HTTPAddr,
		TCPAddress:      appConf.TCPAddr,
		StorePath:       store,
		UploadsPath:     uploads,
		MaxUploadSize:   maxUploadKBytes * 1024,
		Router:          router,
		LogBandwidth:    time.Duration(bandwidthIntervalSeconds) * time.Second,
	}
	s, err := server.New(c)
	if err != nil {
		panic(err)
	}

	now := time.Now().Format("2006-01-02--15-04-05.999999999")
	rnd := make([]byte, 128)
	if _, err := io.ReadFull(crand.Reader, rnd); err != nil {
		panic(err)
	}
	hsh := fnv.New64()
	hsh.Write(rnd)

	appendChatFile := filepath.Join(
		appendChatDir,
		fmt.Sprintf(
			"chat-%s-%s.log",
			now,
			base64.RawURLEncoding.EncodeToString(hsh.Sum(nil)),
		),
	)

	chat := &chatpkg.ChatChannel{}
	history, err := history.New(c.Log, appConf.MaxChatMessages, appendChatFile, chat)
	if err != nil {
		panic(err)
	}
	musicErr := status.New()
	music := music.NewYM(c.Log, musicErr, appConf.YMDir)
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

	s.MustSetUserUpdateHandler(channel.MultiUserUpdateHandler(users, chat))

	go music.SendInterval(time.Millisecond * 1000)
	go music.StateSendInterval(time.Millisecond * 100)
	go music.PlaylistSendInterval(time.Millisecond * 5000)
	go users.SendInterval(time.Millisecond * 500)

	quoteBots := bot.NewBotCollection("quote-bot")
	quoteBots.AddBot("programming", bot.NewBotFunc(bot.ProgrammingQuote))
	quoteBots.AddBot("cats", bot.NewBotFunc(bot.CatQuote))

	weatherBot := bot.NewWttrBot(appConf.WttrCity)

	if appConf.HueIP != "" {
		hue := bot.NewHueBot(
			appConf.HueIP,
			appConf.HuePass,
			[]string{},
		)

		chat.AddBot("hue", hue)
	}

	chat.AddBot("quote", quoteBots)
	chat.AddBot("holidays", bot.NewHolidayBot(appConf.HolidayCountryCode))
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
		panic(err)
	}

	if err := s.Run(); err != nil {
		panic(err)
	}

	fmt.Println("bye...")
}
