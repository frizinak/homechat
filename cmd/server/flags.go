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
)

type Flags struct {
	out io.Writer

	flags struct {
		serve  *flag.FlagSet
		logs   *flag.FlagSet
		hue    *flag.FlagSet
		config *flag.FlagSet
	}

	All struct {
		Mode Mode

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

	f.flags.config = flag.NewFlagSet("config", flag.ExitOnError)
	f.flags.config.SetOutput(f.out)

	flag.CommandLine.SetOutput(f.out)
	flag.StringVar(&f.All.ConfigFile, "c", f.All.ConfigFile, "config file")

	flag.Usage = func() {
		fmt.Fprintln(f.out, "homechat-server")
		flag.PrintDefaults()
		fmt.Fprint(f.out, "\nCommands:\n")
		fmt.Fprintln(f.out, "  - serve | <empty>: Server")
		fmt.Fprintln(f.out, "  - logs:            Append-only logfile operations")
		fmt.Fprintln(f.out, "  - hue:             Configure Philips Hue bridge credentials")
		fmt.Fprintln(f.out, "  - config:          Config options explained")
		fmt.Fprintln(f.out, "  - version:         Print version and exit")
	}
	f.flags.serve.Usage = func() {
		fmt.Fprintln(f.out, "Start the server")
		f.flags.serve.PrintDefaults()
	}
	f.flags.hue.Usage = func() {
		fmt.Fprintln(f.out, "Discover hue bridge and create credentials")
		fmt.Fprintln(f.out, "Automatically stores them in the active server.json")
		f.flags.hue.PrintDefaults()
	}
	f.flags.logs.Usage = func() {
		fmt.Fprintln(f.out, "Print contents of the Append-only logs for now")
		f.flags.logs.PrintDefaults()
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

	f.All.CacheDir = f.AppConf.Directory
	f.All.Store = filepath.Join(f.AppConf.Directory, "chat.log")
	f.All.Uploads = filepath.Join(f.AppConf.Directory, "uploads")

	keyfile := filepath.Join(f.AppConf.Directory, ".rsa_private_server_key")
	key, err := crypto.EnsureKey(keyfile, channel.ServerMinKeySize, channel.ServerKeySize)
	if err != nil {
		return err
	}

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
	}

	if err := f.parseCommand(); err != nil {
		return err
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
		if f.Logs.Dir == "" {
			f.Logs.Dir = *f.AppConf.ChatMessagesAppendOnlyDir
		}
	case "hue":
		f.All.Mode = ModeHue
		if err := f.flags.hue.Parse(args[1:]); err != nil {
			return err
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
