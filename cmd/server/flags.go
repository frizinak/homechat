package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/vars"
)

type PolicyLoader struct {
	policy server.ClientPolicy
	file   string

	rw       sync.RWMutex
	lastLoad time.Time
	list     map[string]string
}

func (p *PolicyLoader) Policy() server.ClientPolicy { return p.policy }

func (p *PolicyLoader) Exists(fp string) (string, error) {
	if err := p.load(); err != nil {
		return "", err
	}

	return p.list[fp], nil
}

func (p *PolicyLoader) load() error {
	if time.Since(p.lastLoad) < time.Second*5 {
		return nil
	}

	p.rw.Lock()
	defer p.rw.Unlock()
	if time.Since(p.lastLoad) < time.Second*5 {
		return nil
	}

	p.lastLoad = time.Now()

	f, err := os.Open(p.file)
	if err != nil {
		return err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Split(bufio.ScanLines)
	n := 0
	list := make(map[string]string)
	for scan.Scan() {
		n++
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}

		lp := strings.Fields(line)
		if len(lp) < 2 {
			return fmt.Errorf("%s: invalid line %d", p.file, n)
		}

		fp := lp[0]
		name := strings.Join(lp[1:], " ")
		if name == "" {
			return fmt.Errorf("%s: invalid line (empty name) %d", p.file, n)
		}

		list[fp] = name
	}

	if err := scan.Err(); err != nil {
		return err
	}

	p.list = list

	return nil
}

type Mode byte

const (
	ModeDefault Mode = iota
	ModeLogs
	ModeHue
	ModeFingerprint
)

type Flags struct {
	out io.Writer

	flags struct {
		serve       *flag.FlagSet
		logs        *flag.FlagSet
		hue         *flag.FlagSet
		fingerprint *flag.FlagSet
		config      *flag.FlagSet
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

		PolicyLoader: &PolicyLoader{
			policy: f.AppConf.ClientPolicy,
			file:   f.AppConf.ClientPolicyFile,
		},
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
	policyFile := filepath.Join(filepath.Dir(f.All.ConfigFile), "client.allowlist")
	var maxUploadKBytes int64 = 1024 * 10
	resave := f.AppConf.Merge(&Config{
		Directory: cache,
		HTTPAddr:  "127.0.0.1:1200",
		TCPAddr:   fmt.Sprintf("%s:%d", addr[0], port+1),
		YMDir:     filepath.Join(cache, "ym"),

		ClientPolicy:     server.PolicyAllow,
		ClientPolicyFile: policyFile,

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
	case "version":
		version := vars.GitVersion
		if version == "" {
			version = vars.Version
		}
		fmt.Fprintf(f.out, "%s (protocol: %s)\n", version, vars.ProtocolVersion)
		os.Exit(0)
	default:
		flag.Usage()
		os.Exit(1)
	}

	return nil
}
