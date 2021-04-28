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
	"github.com/frizinak/homechat/flags"
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
		if line[0] == '#' || line[0] == ';' {
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

	flags       *flags.Set
	CurrentFlag *flags.Set

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
		flags:   flags.NewRoot(output),
	}
	f.All.ConfigFile = defaultConfFile
	f.All.CacheDir = defaultCacheDir

	return f
}

func (f *Flags) Flags() {
	f.flags.Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(&f.All.ConfigFile, "c", f.All.ConfigFile, "config file")

		return func(h *flags.Help) {
			h.Add("Commands:")
			h.Add("  - serve | <empty>: Server")
			h.Add("  - logs:            Append-only logfile operations")
			h.Add("  - hue:             Configure Philips Hue bridge credentials")
			h.Add("  - fingerprint:     Show server publickey fingerprint")
			h.Add("  - config:          Config options explained")
			h.Add("  - version:         Print version and exit")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		if len(args) != 0 {
			set.Usage(1)
		}

		f.All.Mode = ModeDefault
		return nil
	})

	f.flags.Add("serve").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(
			&f.Serve.HTTPDir,
			"http",
			"",
			"Directory the http server will directly serve from [for debugging]",
		)

		return func(h *flags.Help) {
			h.Add("Start the server")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeDefault
		return nil
	})

	f.flags.Add("logs").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(
			&f.Logs.Dir,
			"d",
			"",
			"The directory that contains your logs, defaults to server.json setting",
		)

		return func(h *flags.Help) {
			h.Add("Print contents of the Append-only logs for now")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeLogs
		return nil
	})

	f.flags.Add("hue").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Discover hue bridge and create credentials")
			h.Add("Automatically stores them in the active server.json")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeHue
		return nil
	})

	f.flags.Add("fingerprint").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Show server publickey fingerprint")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeFingerprint
		return nil
	})

	f.flags.Add("config").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add(fmt.Sprintf("Config file used: '%s'", f.All.ConfigFile))
			h.Add("")
			for _, l := range f.AppConf.Help() {
				h.Add(l)
			}
		}
	})

	f.flags.Add("version").Handler(func(set *flags.Set, args []string) error {
		version := vars.GitVersion
		if version == "" {
			version = vars.Version
		}
		fmt.Fprintf(f.out, "%s (protocol: %s)\n", version, vars.ProtocolVersion)
		os.Exit(0)
		return nil
	})
}

func (f *Flags) Parse() error {
	set, trail := f.flags.ParseCommandline()
	f.CurrentFlag = set
	if f.All.ConfigFile == "" {
		return errors.New("please specify a config directory")
	}

	if len(trail) == 1 && trail[len(trail)-1] == "config" {
		set.Usage(0)
	}

	if err := f.validateAppConf(); err != nil {
		return err
	}

	if err := set.Do(); err != nil {
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

	if _, err := os.Stat(f.AppConf.ClientPolicyFile); os.IsNotExist(err) {
		fh, err := os.Create(f.AppConf.ClientPolicyFile)
		if err != nil {
			return err
		}
		defer fh.Close()
		fmt.Fprintln(fh, "# Client allow list")
		fmt.Fprintln(fh, "# One fingerprint and name combination per line")
		fmt.Fprintln(fh, "")
		fmt.Fprintln(fh, "# Example:")
		fmt.Fprintln(fh, "# 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00 username")
		fmt.Fprintln(fh, "")
	}

	f.ServerConf = server.Config{
		Key:               key,
		ProtocolVersion:   vars.ProtocolVersion,
		Log:               log.New(f.out, "", 0),
		HTTPPublicAddress: f.AppConf.HTTPPublicAddr,
		HTTPAddress:       f.AppConf.HTTPBindAddr,
		TCPAddress:        f.AppConf.TCPBindAddr,
		StorePath:         f.All.Store,
		UploadsPath:       f.All.Uploads,
		MaxUploadSize:     *f.AppConf.MaxUploadKBytes * 1024,
		LogBandwidth:      time.Duration(*f.AppConf.BandwidthIntervalSeconds) * time.Second,
		RWFactory:         channel.NewRWFactory(nil),

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

	addr := strings.Split(f.AppConf.HTTPBindAddr, ":")
	if len(addr) != 2 {
		addr = []string{"127.0.0.1", "1200"}
	}

	port, err := strconv.Atoi(addr[1])
	if err != nil {
		return fmt.Errorf("Failed to parse server http address %w", err)
	}

	bandwidthIntervalSeconds := 0
	appendChatDir := filepath.Join(cache, "chatlogs")
	configFileDir, err := filepath.Abs(filepath.Dir(f.All.ConfigFile))
	if err != nil {
		return err
	}

	policyFile := filepath.Join(configFileDir, "client.allowlist")
	var maxUploadKBytes int64 = 1024 * 10
	resave := f.AppConf.Merge(&Config{
		Directory:      cache,
		HTTPPublicAddr: fmt.Sprintf("%s:%d", addr[0], port),
		HTTPBindAddr:   "127.0.0.1:1200",
		TCPBindAddr:    fmt.Sprintf("%s:%d", addr[0], port+1),
		YMDir:          filepath.Join(cache, "ym"),

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
