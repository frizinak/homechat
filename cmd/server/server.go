package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/frizinak/gotls/simplehttp"
	"github.com/frizinak/homechat/bot"
	"github.com/frizinak/homechat/bound"
	"github.com/frizinak/homechat/server"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/chat"
	"github.com/frizinak/homechat/server/channel/history"
	"github.com/frizinak/homechat/server/channel/music"
	"github.com/frizinak/homechat/server/channel/ping"
	"github.com/frizinak/homechat/server/channel/status"
	"github.com/frizinak/homechat/server/channel/upload"
	"github.com/frizinak/homechat/server/channel/users"
	"github.com/frizinak/homechat/vars"
)

type Config struct {
	Directory string
	HTTPAddr  string
	YMDir     string

	WttrCity           string
	HolidayCountryCode string

	HueIP   string
	HuePass string
}

func (c *Config) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(c); err != nil {
		return fmt.Errorf("Failed to parse server config %s: %w", file, err)
	}
	return nil
}

func (c *Config) Encode(file string) error {
	tmp := file + ".tmp"
	err := func() error {
		f, err := os.Create(tmp)
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "    ")
		if err := enc.Encode(c); err != nil {
			return err
		}
		return nil
	}()
	if err != nil {
		return err
	}

	return os.Rename(tmp, file)
}

func (c *Config) Merge(def *Config) bool {
	resave := false
	if c.WttrCity == "" {
		resave = true
		c.WttrCity = def.WttrCity
	}
	if c.HolidayCountryCode == "" {
		resave = true
		c.HolidayCountryCode = def.HolidayCountryCode
	}
	if c.YMDir == "" {
		resave = true
		c.YMDir = def.YMDir
	}
	if c.Directory == "" {
		resave = true
		c.Directory = def.Directory
	}
	if c.HTTPAddr == "" {
		resave = true
		c.HTTPAddr = def.HTTPAddr
	}
	return resave
}

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

	resave := appConf.Merge(&Config{
		Directory: cache,
		HTTPAddr:  "127.0.0.1:1200",
		YMDir:     filepath.Join(cache, "ym"),

		WttrCity:           "tashkent",
		HolidayCountryCode: "UZ",
	})

	if resave {
		if err := appConf.Encode(confFile); err != nil {
			panic(err)
		}
	}

	store := filepath.Join(appConf.Directory, "chat.log")
	uploads := filepath.Join(appConf.Directory, "uploads")
	os.MkdirAll(uploads, 0700)

	addr := strings.Split(appConf.HTTPAddr, ":")
	if len(addr) != 2 {
		panic("invalid address")
	}

	port, err := strconv.Atoi(addr[1])
	if err != nil {
		panic(err)
	}
	port++
	tcp := fmt.Sprintf("%s:%d", addr[0], port)

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
		TCPAddress:      tcp,
		StorePath:       store,
		UploadsPath:     uploads,
		MaxUploadSize:   1024 * 1024 * 1024,
		Router:          router,
		LogBandwidth:    time.Minute,
	}
	s, err := server.New(c)
	if err != nil {
		panic(err)
	}

	history := history.New(100000, 1000)
	musicErr := status.New()
	music := music.NewYM(c.Log, musicErr, appConf.YMDir)
	chat := chat.New(c.Log, history)
	history.SetOutput(chat)
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

	fmt.Printf("Starting server on http://%s tcp://%s\n", c.HTTPAddress, c.TCPAddress)
	if err := s.Init(); err != nil {
		panic(err)
	}

	panic(s.Run())
}
