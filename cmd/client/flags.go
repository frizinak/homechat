package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/backend/tcp"
	"github.com/frizinak/homechat/client/backend/ws"
	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/vars"
	"github.com/frizinak/libym/di"
	"github.com/google/shlex"
)

type Mode byte

const (
	ModeDefault Mode = iota
	ModeMusic
	ModeUpload
	ModeFingerprint
	ModeMusicNode
)

type Flags struct {
	out io.Writer

	All struct {
		Key        *crypto.Key
		ConfigDir  string
		CacheDir   string
		ConfigFile string
		KeymapFile string
		Linemode   bool

		Mode Mode

		Interactive bool
		OneOff      string
	}
	Chat struct {
		NotifyCommand []string
	}
	Upload struct {
		Msg  string
		File string
	}
	MusicNode struct {
		CacheDir   string
		LowLatency bool
	}

	flags struct {
		chat        *flag.FlagSet
		music       *flag.FlagSet
		upload      *flag.FlagSet
		config      *flag.FlagSet
		fingerprint *flag.FlagSet
		musicnode   *flag.FlagSet
	}

	AppConf         *Config
	ClientConf      client.Config
	TCPConf         tcp.Config
	WSConf          ws.Config
	MusicNodeConfig di.Config

	Keymap Keymap
}

func NewFlags(output io.Writer, defaultConfigDir, defaultCacheDir string, interactive bool) *Flags {
	f := &Flags{
		out:     output,
		AppConf: &Config{},
	}
	f.All.ConfigDir = defaultConfigDir
	f.All.CacheDir = defaultCacheDir
	f.All.Interactive = interactive

	return f
}

func (f *Flags) Flags() {
	f.flags.chat = flag.NewFlagSet("chat", flag.ExitOnError)
	f.flags.chat.SetOutput(f.out)
	f.flags.chat.BoolVar(
		&f.All.Linemode,
		"l",
		false,
		"when piping treat every line as a new message, thus streaming line by line",
	)

	f.flags.music = flag.NewFlagSet("music", flag.ExitOnError)
	f.flags.music.SetOutput(f.out)

	f.flags.upload = flag.NewFlagSet("upload", flag.ExitOnError)
	f.flags.upload.SetOutput(f.out)
	f.flags.upload.StringVar(&f.Upload.Msg, "m", "Download: ", "prefix upload url with this message")

	f.flags.config = flag.NewFlagSet("config", flag.ExitOnError)
	f.flags.config.SetOutput(f.out)

	f.flags.fingerprint = flag.NewFlagSet("fingerprint", flag.ExitOnError)
	f.flags.fingerprint.SetOutput(f.out)

	f.flags.musicnode = flag.NewFlagSet("musicnode", flag.ExitOnError)
	f.flags.musicnode.BoolVar(&f.MusicNode.LowLatency, "low-latency", false, "Enable low latency mode")
	f.flags.musicnode.SetOutput(f.out)

	flag.CommandLine.SetOutput(f.out)
	flag.StringVar(&f.All.ConfigDir, "c", f.All.ConfigDir, "config directory")

	flag.Usage = func() {
		fmt.Fprintln(f.out, "homechat")
		flag.PrintDefaults()
		fmt.Fprint(f.out, "\nCommands:\n")
		fmt.Fprintln(f.out, "  - chat | <empty>: Chat client")
		fmt.Fprintln(f.out, "  - music:          Music client")
		fmt.Fprintln(f.out, "  - upload:         Upload a file from stdin or commandline to chat")
		fmt.Fprintln(f.out, "  - config:         Config options explained")
		fmt.Fprintln(f.out, "  - fingerprint:    Show your and the server's trusted publickey fingerprint")
		fmt.Fprintln(f.out, "  - musicnode:      Run a music node")
		fmt.Fprintln(f.out, "  - version:        Print version and exit")
	}
	f.flags.chat.Usage = func() {
		f.flags.chat.PrintDefaults()
		fmt.Fprintln(f.out, "Run interactively")
		fmt.Fprintln(f.out, " - homechat chat")
		fmt.Fprintln(f.out, "")
		fmt.Fprintln(f.out, "Send message and exit")
		fmt.Fprintln(f.out, " - homechat chat <message to send>")
		fmt.Fprintln(f.out, " - command | homechat chat")
	}
	f.flags.music.Usage = func() {
		f.flags.music.PrintDefaults()
		fmt.Fprintln(f.out, "Run interactively")
		fmt.Fprintln(f.out, " - homechat music")
		fmt.Fprintln(f.out, "")
		fmt.Fprintln(f.out, "Send command and exit")
		fmt.Fprintln(f.out, " - homechat music <command to send>")
		fmt.Fprintln(f.out, " - command | homechat music")
	}
	f.flags.upload.Usage = func() {
		f.flags.upload.PrintDefaults()
		fmt.Fprintln(f.out, "Usage")
		fmt.Fprintln(f.out, " - homechat upload <filepath>")
		fmt.Fprintln(f.out, " - command | homechat upload")
	}
	f.flags.config.Usage = func() {
		fmt.Fprintf(f.out, "Config file used: '%s'\n\n", f.All.ConfigFile)
		if err := f.AppConf.Help(f.out); err != nil {
			panic(err)
		}
	}
	f.flags.fingerprint.Usage = func() {
		fmt.Fprintln(f.out, "Show your and the server's trusted publickey fingerprint")
		f.flags.fingerprint.PrintDefaults()
	}
	f.flags.musicnode.Usage = func() {
		f.flags.musicnode.PrintDefaults()
		fmt.Fprintln(f.out, "Run a music node")
	}
}

func (f *Flags) SaveConfig() error {
	return f.AppConf.Encode(f.All.ConfigFile)
}

func (f *Flags) Parse() error {
	flag.Parse()
	if f.All.ConfigDir == "" {
		return errors.New("please specify a config directory")
	}

	f.All.ConfigFile = filepath.Join(f.All.ConfigDir, "client.json")
	f.All.KeymapFile = filepath.Join(f.All.ConfigDir, "keymap.json")
	if flag.Arg(0) == "config" {
		f.flags.config.Usage()
		os.Exit(0)
	}
	os.MkdirAll(f.All.ConfigDir, 0o755)

	if err := f.validateAppConf(); err != nil {
		return err
	}
	if err := f.validateKeymap(); err != nil {
		return err
	}

	keyfile := filepath.Join(f.All.ConfigDir, ".rsa_private_key")
	key, err := crypto.EnsureKey(keyfile, channel.ClientMinKeySize, channel.ClientKeySize)
	if err != nil {
		return err
	}
	f.All.Key = key

	f.TCPConf = tcp.Config{TCPAddr: f.AppConf.ServerTCPAddress}
	f.WSConf = ws.Config{
		TLS:    false,
		Domain: f.AppConf.ServerAddress,
		Path:   "ws",
	}

	f.ClientConf.Key = key
	f.ClientConf.Name = strings.TrimSpace(f.AppConf.Username)
	f.ClientConf.Proto = channel.ProtoBinary
	f.ClientConf.ServerURL = "http://" + f.AppConf.ServerAddress
	f.ClientConf.ServerFingerprint = f.AppConf.ServerFingerprint
	f.ClientConf.History = uint16(f.AppConf.MaxMessages)

	if f.ClientConf.Name == "" {
		return fmt.Errorf("please specify your desired username in %s", f.All.ConfigFile)
	}

	n, err := shlex.Split(*f.AppConf.NotifyCommand)
	if err != nil {
		return err
	}
	f.Chat.NotifyCommand = n

	if len(f.AppConf.ServerAddress) == 0 {
		return fmt.Errorf("please specify the server ip:port in %s", f.All.ConfigFile)
	}

	switch f.AppConf.NotifyWhen {
	case NotifyDefault, NotifyAlways:
	default:
		return fmt.Errorf("please specify a valid NotifyWhen in %s", f.All.ConfigFile)
	}

	if err := f.parseCommand(); err != nil {
		return err
	}

	f.ClientConf.Channels = []string{
		vars.PingChannel,
		vars.UserChannel,
		vars.HistoryChannel,
		vars.ChatChannel,
	}

	f.MusicNode.CacheDir = filepath.Join(f.All.CacheDir, "musicnode")
	f.MusicNodeConfig = di.Config{
		Log:           log.New(os.Stdout, "", 0),
		StorePath:     f.MusicNode.CacheDir,
		BackendLogger: ioutil.Discard,
		AutoSave:      false,
		SimpleOutput:  ioutil.Discard,
	}

	switch f.All.Mode {
	case ModeDefault:
	case ModeMusic:
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.PingChannel,
			vars.UserChannel,
			vars.MusicChannel,
			vars.MusicStateChannel,
			vars.MusicSongChannel,
			vars.MusicPlaylistChannel,
			vars.MusicErrorChannel,
		}
	case ModeUpload:
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{}
	case ModeMusicNode:
		os.MkdirAll(f.MusicNode.CacheDir, 0o755)
		f.ClientConf.Name += "-music-node"
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.PingChannel,
			vars.UserChannel,
			vars.MusicChannel,
			vars.MusicStateChannel,
			vars.MusicSongChannel,
			vars.MusicPlaylistChannel,
			vars.MusicErrorChannel,
			vars.MusicNodeChannel,
		}

	}

	if f.All.OneOff != "" || !f.All.Interactive {
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{}
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

	addr := strings.Split(f.AppConf.ServerAddress, ":")
	if len(addr) != 2 {
		addr = []string{"127.0.0.1", "1200"}
	}
	port, err := strconv.Atoi(addr[1])
	if err != nil {
		panic(fmt.Errorf("Failed to parse server http address %w", err))
	}

	notifyCmd := "notify-send 'HomeChat' '%u: %m'"
	resave := f.AppConf.Merge(&Config{
		NotifyCommand:    &notifyCmd,
		NotifyWhen:       NotifyDefault,
		ServerAddress:    "127.0.0.1:1200",
		ServerTCPAddress: fmt.Sprintf("%s:%d", addr[0], port+1),
		Username:         "",
		MaxMessages:      250,
	})

	if !resave {
		return nil
	}

	return f.AppConf.Encode(f.All.ConfigFile)
}

func (f *Flags) validateKeymap() error {
	f.Keymap = make(Keymap)
	if err := f.Keymap.Decode(f.All.KeymapFile); err != nil {
		if os.IsNotExist(err) {
			if err := f.Keymap.Encode(f.All.KeymapFile); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	defaultKeymap := Keymap{
		PageDown:   "ctrl-d",
		PageUp:     "ctrl-u",
		ScrollDown: "ctrl-j",
		ScrollUp:   "ctrl-k",

		Backspace:  "backspace",
		Completion: "tab",
		Quit:       "ctrl-q",
		ClearInput: "ctrl-c",
		Submit:     "enter",
		InputDown:  "down",
		InputUp:    "up",

		MusicPlaylistCompletion: "tab",
		MusicVolumeUp:           "up",
		MusicVolumeDown:         "down",
		MusicNext:               "right",
		MusicPrevious:           "left",
		MusicPause:              "space",
		MusicSeekForward:        "]",
		MusicSeekBackward:       "[",
	}

	if resave := f.Keymap.Merge(defaultKeymap); resave {
		if err := f.Keymap.Encode(f.All.KeymapFile); err != nil {
			return err
		}
	}

	for i := range f.Keymap {
		if _, ok := defaultKeymap[i]; !ok {
			return fmt.Errorf("unknown action '%s' in keymap", i)
		}
	}

	return nil
}

func (f *Flags) parseCommand() error {
	args := flag.Args()
	switch flag.Arg(0) {
	case "", "chat":
		f.All.Mode = ModeDefault
		if len(args) == 0 {
			break
		}
		if err := f.flags.chat.Parse(args[1:]); err != nil {
			return err
		}
		f.All.OneOff = strings.Join(f.flags.chat.Args(), " ")
	case "music":
		f.All.Mode = ModeMusic
		if err := f.flags.music.Parse(args[1:]); err != nil {
			return err
		}
		f.All.OneOff = strings.Join(f.flags.music.Args(), " ")
	case "upload":
		f.All.Mode = ModeUpload
		if err := f.flags.upload.Parse(args[1:]); err != nil {
			return err
		}
		f.Upload.File = f.flags.upload.Arg(0)
		if f.Upload.File == "" && f.All.Interactive {
			return errors.New("please provide a file")
		}
	case "fingerprint":
		f.All.Mode = ModeFingerprint
		if err := f.flags.fingerprint.Parse(args[1:]); err != nil {
			return err
		}
	case "musicnode":
		f.All.Mode = ModeMusicNode
		if err := f.flags.musicnode.Parse(args[1:]); err != nil {
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
