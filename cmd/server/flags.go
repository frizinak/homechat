package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/vars"
)

type Mode byte

const (
	ModeDefault Mode = iota
	ModeLogs
	ModeHue
	ModeFingerprint
	ModeMusicFiles
	ModeMusicDownloads
)

type Flags struct {
	out io.Writer

	flags struct {
		serve          *flag.FlagSet
		logs           *flag.FlagSet
		hue            *flag.FlagSet
		fingerprint    *flag.FlagSet
		config         *flag.FlagSet
		music          *flag.FlagSet
		musicFiles     *flag.FlagSet
		musicDownloads *flag.FlagSet
	}

	All struct {
		Mode Mode

		Key        *crypto.Key
		ConfigFile string
		CacheDir   string
		Store      string
		Uploads    string
	}

	Serve struct {
		HTTPDir string
	}

	Logs struct {
		Dir string
	}

	Music struct {
		Dir string
	}

	MusicFiles struct {
		Stat bool
		KiB  bool
	}

	AppConf    *Config
	ServerConf server.Config
}

func NewFlags(output io.Writer, defaultConfFile, defaultCacheDir string) *Flags {
	f := &Flags{
		out:     output,
		AppConf: &Config{},
	}
	f.All.ConfigFile = defaultConfFile
	f.All.CacheDir = defaultCacheDir

	return f
}

func (f *Flags) Flags() {
	f.flags.serve = flag.NewFlagSet("serve", flag.ExitOnError)
	f.flags.serve.SetOutput(f.out)
	f.flags.serve.StringVar(
		&f.Serve.HTTPDir,
		"http",
		"",
		"Directory the http server will directly serve from [for debugging]",
	)

	f.flags.logs = flag.NewFlagSet("logs", flag.ExitOnError)
	f.flags.logs.SetOutput(f.out)
	f.flags.logs.StringVar(
		&f.Logs.Dir,
		"d",
		"",
		"The directory that contains your logs, defaults to server.json setting",
	)
	f.flags.hue = flag.NewFlagSet("hue", flag.ExitOnError)
	f.flags.hue.SetOutput(f.out)

	f.flags.fingerprint = flag.NewFlagSet("fingerprint", flag.ExitOnError)
	f.flags.fingerprint.SetOutput(f.out)

	f.flags.config = flag.NewFlagSet("config", flag.ExitOnError)
	f.flags.config.SetOutput(f.out)

	f.flags.music = flag.NewFlagSet("music", flag.ExitOnError)
	f.flags.music.SetOutput(f.out)

	f.flags.musicFiles = flag.NewFlagSet("music files", flag.ExitOnError)
	f.flags.musicFiles.BoolVar(&f.MusicFiles.Stat, "s", false, "stat files and print disk usage")
	f.flags.musicFiles.BoolVar(&f.MusicFiles.KiB, "k", false, "print size in KiB, ignored when -s is not passed")
	f.flags.musicFiles.SetOutput(f.out)

	f.flags.musicDownloads = flag.NewFlagSet("music downloads", flag.ExitOnError)
	f.flags.musicDownloads.SetOutput(f.out)

	flag.CommandLine.SetOutput(f.out)
	flag.StringVar(&f.All.ConfigFile, "c", f.All.ConfigFile, "config file")

	flag.Usage = func() {
		fmt.Fprintln(f.out, "homechat-server")
		flag.PrintDefaults()
		fmt.Fprint(f.out, "\nCommands:\n")
		fmt.Fprintln(f.out, "  - serve | <empty>: Server")
		fmt.Fprintln(f.out, "  - logs:            Append-only logfile operations")
		fmt.Fprintln(f.out, "  - hue:             Configure Philips Hue bridge credentials")
		fmt.Fprintln(f.out, "  - fingerprint:     Show server publickey fingerprint")
		fmt.Fprintln(f.out, "  - music:           Music/libym related subcommands")
		fmt.Fprintln(f.out, "  - config:          Config options explained")
		fmt.Fprintln(f.out, "  - version:         Print version and exit")
	}
	f.flags.serve.Usage = func() {
		fmt.Fprintln(f.out, "Start the server")
		f.flags.serve.PrintDefaults()
	}
	f.flags.logs.Usage = func() {
		fmt.Fprintln(f.out, "Print contents of the Append-only logs for now")
		f.flags.logs.PrintDefaults()
	}
	f.flags.hue.Usage = func() {
		fmt.Fprintln(f.out, "Discover hue bridge and create credentials")
		fmt.Fprintln(f.out, "Automatically stores them in the active server.json")
		f.flags.hue.PrintDefaults()
	}
	f.flags.fingerprint.Usage = func() {
		fmt.Fprintln(f.out, "Show server publickey fingerprint")
		f.flags.fingerprint.PrintDefaults()
	}
	f.flags.music.Usage = func() {
		fmt.Fprintln(f.out, "music")
		f.flags.music.PrintDefaults()
		fmt.Fprint(f.out, "\nCommands:\n")
		fmt.Fprintln(f.out, "  - files:     list unused files (not in a playlist)")
		fmt.Fprintln(f.out, "  - downloads: list songs that are not (yet) downloaded")
	}
	f.flags.musicFiles.Usage = func() {
		fmt.Fprintln(f.out, "List unused files")
		f.flags.musicFiles.PrintDefaults()
	}
	f.flags.musicDownloads.Usage = func() {
		fmt.Fprintln(f.out, "List songs that are not (yet) downloaded")
		f.flags.musicDownloads.PrintDefaults()
	}
	f.flags.config.Usage = func() {
		fmt.Fprintf(f.out, "Config file used: '%s'\n\n", f.All.ConfigFile)
		if err := f.AppConf.Help(f.out); err != nil {
			panic(err)
		}
	}
}

func (f *Flags) Parse() error {
	flag.Parse()
	if f.All.ConfigFile == "" {
		return errors.New("please specify a config directory")
	}

	if flag.Arg(0) == "config" {
		f.flags.config.Usage()
		os.Exit(0)
	}

	if err := f.validateAppConf(); err != nil {
		return err
	}

	if err := f.parseCommand(); err != nil {
		return err
	}

	f.Music.Dir = f.AppConf.YMDir

	f.All.CacheDir = f.AppConf.Directory
	f.All.Store = filepath.Join(f.AppConf.Directory, "chat.log")
	f.All.Uploads = filepath.Join(f.AppConf.Directory, "uploads")
	if f.Logs.Dir == "" {
		f.Logs.Dir = *f.AppConf.ChatMessagesAppendOnlyDir
	}

	mkdir := func(name, path string) error {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("failed to create %s directory '%s': %w", name, path, err)
		}
		return nil
	}

	dirs := map[string]string{
		"cache":   f.All.CacheDir,
		"uploads": f.All.Uploads,
		"logs":    f.Logs.Dir,
	}

	if f.All.Mode != ModeDefault {
		dirs["logs"] = ""
	}

	for n, p := range dirs {
		if p == "" {
			continue
		}
		if err := mkdir(n, p); err != nil {
			return err
		}
	}

	keyfile := filepath.Join(f.AppConf.Directory, ".rsa_private_server_key")
	key, err := crypto.EnsureKey(keyfile, channel.ServerMinKeySize, channel.ServerKeySize)
	if err != nil {
		return err
	}
	f.All.Key = key

	f.ServerConf = server.Config{
		Key:             key,
		ProtocolVersion: vars.ProtocolVersion,
		Log:             log.New(f.out, "", 0),
		HTTPAddress:     f.AppConf.HTTPAddr,
		TCPAddress:      f.AppConf.TCPAddr,
		StorePath:       f.All.Store,
		UploadsPath:     f.All.Uploads,
		MaxUploadSize:   *f.AppConf.MaxUploadKBytes * 1024,
		LogBandwidth:    time.Duration(*f.AppConf.BandwidthIntervalSeconds) * time.Second,
		RWFactory:       channel.NewRWFactory(nil),
	}

	return nil
}

func (f *Flags) validateAppConf() error {
	if err := f.AppConf.Decode(f.All.ConfigFile); err != nil {
		if os.IsNotExist(err) {
			if err := f.AppConf.Encode(f.All.ConfigFile); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	cache := f.AppConf.Directory
	if cache == "" {
		cache = f.All.CacheDir
	}

	addr := strings.Split(f.AppConf.HTTPAddr, ":")
	if len(addr) != 2 {
		addr = []string{"127.0.0.1", "1200"}
	}

	port, err := strconv.Atoi(addr[1])
	if err != nil {
		return fmt.Errorf("Failed to parse server http address %w", err)
	}

	bandwidthIntervalSeconds := 0
	appendChatDir := filepath.Join(cache, "chatlogs")
	var maxUploadKBytes int64 = 1024 * 10
	resave := f.AppConf.Merge(&Config{
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

	if f.AppConf.Directory == "" {
		err = fmt.Errorf("please specify a directory in %s", f.All.ConfigFile)
	}

	if !resave {
		return err
	}

	if err := f.AppConf.Encode(f.All.ConfigFile); err != nil {
		return err
	}

	return err
}

func (f *Flags) parseCommand() error {
	args := flag.Args()
	switch flag.Arg(0) {
	case "", "serve":
		f.All.Mode = ModeDefault
		if len(args) == 0 {
			break
		}
		if err := f.flags.serve.Parse(args[1:]); err != nil {
			return err
		}
	case "logs":
		f.All.Mode = ModeLogs
		if err := f.flags.logs.Parse(args[1:]); err != nil {
			return err
		}
	case "hue":
		f.All.Mode = ModeHue
		if err := f.flags.hue.Parse(args[1:]); err != nil {
			return err
		}
	case "fingerprint":
		f.All.Mode = ModeFingerprint
		if err := f.flags.fingerprint.Parse(args[1:]); err != nil {
			return err
		}
	case "music":
		if len(args) <= 1 {
			f.flags.music.Usage()
			os.Exit(1)
		}
		switch args[1] {
		case "files":
			f.All.Mode = ModeMusicFiles
			if err := f.flags.musicFiles.Parse(args[2:]); err != nil {
				return err
			}
		case "downloads":
			f.All.Mode = ModeMusicDownloads
			if err := f.flags.musicDownloads.Parse(args[2:]); err != nil {
				return err
			}
		default:
			f.flags.music.Usage()
			os.Exit(1)
		}
	case "version":
		fmt.Fprintf(f.out, "%s (protocol: %s)\n", vars.Version, vars.ProtocolVersion)
		os.Exit(0)
	default:
		flag.Usage()
		os.Exit(1)
	}

	return nil
}
