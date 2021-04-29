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
	"github.com/frizinak/homechat/flags"
	"github.com/frizinak/homechat/open"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/vars"
	"github.com/frizinak/libym/di"
	"github.com/google/shlex"
)

type Mode byte

const (
	ModeDefault Mode = iota
	ModeUpload

	ModeFingerprint

	ModeMusicRemote
	ModeMusicClient
	ModeMusicNode
	ModeMusicDownload

	ModeMusicRemoteCurrent

	ModeMusicInfoFiles
	ModeMusicInfoDownloads
	ModeMusicInfoSongs

	ModeUpdate
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
		Msg string
	}
	MusicNode struct {
		CacheDir   string
		Socket     string
		LowLatency bool
	}
	MusicClient struct {
		Offline bool
	}
	MusicInfo struct {
		DefaultDir string
		Dir        string
		ForClient  bool
	}
	MusicInfoFiles struct {
		Stat bool
		KiB  bool
	}
	MusicInfoSongs struct {
		Stat bool
		KiB  bool
	}
	MusicRemoteCurrent struct {
		N uint
	}
	Update struct {
		Path string
	}

	flags       *flags.Set
	CurrentFlag *flags.Set

	AppConf         *Config
	ClientConf      client.Config
	TCPConf         tcp.Config
	WSConf          ws.Config
	MusicNodeConfig di.Config

	Opener *open.Opener

	Keymap Keymap
}

func NewFlags(output io.Writer, defaultConfigDir, defaultCacheDir string, interactive bool) *Flags {
	f := &Flags{
		out:     output,
		AppConf: &Config{},
		flags:   flags.NewRoot(output),
	}
	f.All.ConfigDir = defaultConfigDir
	f.All.CacheDir = defaultCacheDir
	f.All.Interactive = interactive

	return f
}

func (f *Flags) Flags() {
	f.flags.Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(&f.All.ConfigDir, "c", f.All.ConfigDir, "config directory")

		return func(h *flags.Help) {
			h.Add("Commands:")
			h.Add("  - chat | <empty>: Chat client")
			h.Add("  - music:          Music commands")
			h.Add("  - upload:         Upload a file from stdin or commandline to chat")
			h.Add("  - config:         Config options explained")
			h.Add("  - fingerprint:    Show your and the server's trusted publickey fingerprint")
			h.Add("  - update:         Update your client to the latest version")
			h.Add("  - version:        Print version and exit")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		if len(args) != 0 {
			set.Usage(1)
		}

		f.All.Mode = ModeDefault
		return nil
	})

	f.flags.Add("update").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(
			&f.Update.Path,
			"d",
			"",
			"write new binary to destination instead of overwriting the current one",
		)

		return func(h *flags.Help) {
			h.Add("Update your homechat client")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeUpdate
		return nil
	})

	f.flags.Add("chat").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.BoolVar(
			&f.All.Linemode,
			"l",
			false,
			"when piping treat every line as a new message, thus streaming line by line",
		)

		return func(h *flags.Help) {
			h.Add("Run interactively")
			h.Add(" - homechat chat")
			h.Add("")
			h.Add("Send message and exit")
			h.Add(" - homechat chat <message to send>")
			h.Add(" - command | homechat chat")
			h.Add("")
			h.Add("Chat commands:")
			h.Add(" - @username:     whisper someone if message starts with this mention")
			h.Add(" - ... @username: mention a user (notify)")
			h.Add(" - @...<tab>:     autocomplete a mention/whisper")
			h.Add(" - /command:      send a bot command (use /help to get a listing of bots)")
			h.Add(" - //command:     same as the above but other users see neither your command nor the bots' reply")
			h.Add(" - ?query:        search and jump to matches of your query")
			h.Add("                  repeat the query to jump to the next occurrence")
			h.Add(" - %n             open link with id n")
			h.Add("")
			h.Add("See keys.json for other commands/keybinds")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeDefault
		f.All.OneOff = strings.Join(args, " ")
		return nil
	})

	f.flags.Add("upload").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.StringVar(&f.Upload.Msg, "m", "Download: ", "prefix upload url with this message")

		return func(h *flags.Help) {
			h.Add("Usage")
			h.Add(" - homechat upload <filepath>")
			h.Add(" - command | homechat upload")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeUpload
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

	f.flags.Add("fingerprint").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Show your and the server's trusted publickey fingerprint")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeFingerprint
		return nil
	})

	music := f.flags.Add("music").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Commands:")
			h.Add("  - remote | <empty>:   control server music player (main intended usage)")
			h.Add("  - node:               run a music node in sync with the server")
			h.Add("  - client:             start a local music player with local queue but playlists on server")
			h.Add("  - download:           download music from server")
			h.Add("  - info:               libym related commands")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		if len(args) != 0 {
			set.Usage(1)
		}

		f.All.Mode = ModeMusicRemote
		return nil
	})

	musicRemote := music.Add("remote").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Commands:")
			h.Add("  - <empty>:   control server music player (main intended usage)")
			h.Add("  - current:   print current song")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicRemote
		f.All.OneOff = strings.Join(args, " ")
		return nil
	})

	musicRemote.Add("current").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.UintVar(&f.MusicRemoteCurrent.N, "n", 0, "Amount of time")

		return func(h *flags.Help) {
			h.Add("Continuously logs the current song or -n times")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicRemoteCurrent
		return nil
	})

	music.Add("node").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.BoolVar(&f.MusicNode.LowLatency, "low-latency", false, "Enable low latency mode")

		return func(h *flags.Help) {
			h.Add("Run a music node")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicNode
		return nil
	})

	music.Add("client").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.BoolVar(&f.MusicClient.Offline, "offline", false, "Dont connect to server")

		return func(h *flags.Help) {
			h.Add("Run a music client")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicClient
		return nil
	})

	music.Add("download").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("Download the given playlist")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicDownload
		return nil
	})

	musicInfo := music.Add("info").Define(func(fl *flag.FlagSet) flags.HelpCB {
		def := filepath.Join(f.All.CacheDir, "ym")
		f.MusicInfo.DefaultDir = def
		fl.StringVar(&f.MusicInfo.Dir, "d", "", fmt.Sprintf("libym directory (default \"%s\")", def))
		fl.BoolVar(
			&f.MusicInfo.ForClient,
			"c",
			false,
			"use client/node libym directory as opposed to the server dir\nthis overrides -d",
		)

		return func(h *flags.Help) {
			h.Add("music info")
			h.Add("")
			h.Add("Note: these commands operate on the server libym database")
			h.Add("      they will work on the client equivalent as well")
			h.Add("      if you use any of `homechat music {download,client,node}`")
			h.Add("")
			h.Add("Commands:")
			h.Add("  - files:     list unused files (not in a playlist)")
			h.Add("  - downloads: list songs that are not (yet) downloaded")
			h.Add("  - songs:     list all songs")
		}
	})

	musicInfo.Add("files").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.BoolVar(&f.MusicInfoFiles.Stat, "s", false, "stat files and print disk usage")
		fl.BoolVar(&f.MusicInfoFiles.KiB, "k", false, "print size in KiB, ignored when -s is not passed")

		return func(h *flags.Help) {
			h.Add("List unused files")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicInfoFiles
		return nil
	})

	musicInfo.Add("downloads").Define(func(fl *flag.FlagSet) flags.HelpCB {
		return func(h *flags.Help) {
			h.Add("List songs that are not (yet) downloaded")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicInfoDownloads
		return nil
	})

	musicInfo.Add("songs").Define(func(fl *flag.FlagSet) flags.HelpCB {
		fl.BoolVar(&f.MusicInfoSongs.Stat, "s", false, "stat files and print disk usage")
		fl.BoolVar(&f.MusicInfoSongs.KiB, "k", false, "print size in KiB, ignored when -s is not passed")

		return func(h *flags.Help) {
			h.Add("List all songs")
		}
	}).Handler(func(set *flags.Set, args []string) error {
		f.All.Mode = ModeMusicInfoSongs
		return nil
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

func (f *Flags) SaveConfig() error {
	return f.AppConf.Encode(f.All.ConfigFile)
}

func (f *Flags) Parse() error {
	set, trail := f.flags.ParseCommandline()
	f.CurrentFlag = set

	if f.All.ConfigDir == "" {
		return errors.New("please specify a config directory")
	}

	f.All.ConfigFile = filepath.Join(f.All.ConfigDir, "client.json")
	f.All.KeymapFile = filepath.Join(f.All.ConfigDir, "keymap.json")
	if len(trail) == 1 && trail[len(trail)-1] == "config" {
		set.Usage(0)
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
	f.ClientConf.Name = f.AppConf.Username
	f.ClientConf.Proto = channel.ProtoBinary
	f.ClientConf.ServerURL = "http://" + f.AppConf.ServerAddress
	f.ClientConf.ServerFingerprint = f.AppConf.ServerFingerprint
	f.ClientConf.History = uint16(f.AppConf.MaxMessages)

	if f.ClientConf.Name == "" {
		return fmt.Errorf("please specify your desired username in %s", f.All.ConfigFile)
	}

	notify, err := shlex.Split(*f.AppConf.NotifyCommand)
	if err != nil {
		return err
	}
	f.Chat.NotifyCommand = notify

	f.Opener = open.New()
	openurl, err := shlex.Split(f.AppConf.OpenURLCommand)
	if err != nil {
		return err
	}
	if len(openurl) != 0 {
		f.Opener.SetOpenURL(func(u string) error {
			for i := range openurl {
				openurl[i] = strings.ReplaceAll(openurl[i], "%u", u)
			}

			return open.Run(openurl[0], openurl[1:]...)
		})
	}

	if len(f.AppConf.ServerAddress) == 0 {
		return fmt.Errorf("please specify the server ip:port in %s", f.All.ConfigFile)
	}

	if f.MusicInfo.Dir == "" {
		f.MusicInfo.Dir = f.MusicInfo.DefaultDir
		if f.MusicInfo.ForClient {
			f.MusicInfo.Dir = f.AppConf.MusicDownloads
		}
	}

	switch f.AppConf.NotifyWhen {
	case NotifyDefault, NotifyAlways:
	default:
		return fmt.Errorf("please specify a valid NotifyWhen in %s", f.All.ConfigFile)
	}

	if err := set.Do(); err != nil {
		return err
	}

	f.ClientConf.Channels = []string{
		vars.PingChannel,
		vars.UserChannel,
		vars.HistoryChannel,
		vars.ChatChannel,
		vars.TypingChannel,
	}

	f.MusicNode.CacheDir = f.AppConf.MusicDownloads
	f.MusicNode.Socket = f.AppConf.MusicSocketFile
	f.MusicNodeConfig = di.Config{
		Log:           log.New(ioutil.Discard, "", 0),
		StorePath:     f.MusicNode.CacheDir,
		SocketPath:    f.MusicNode.Socket,
		BackendLogger: ioutil.Discard,
		AutoSave:      false,
		SimpleOutput:  ioutil.Discard,
	}

	switch f.All.Mode {
	case ModeDefault:
	case ModeUpload:
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{}
	case ModeUpdate:
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.UpdateChannel,
		}
	case ModeMusicRemote:
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

	case ModeMusicRemoteCurrent:
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.MusicStateChannel,
			vars.MusicSongChannel,
		}

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

	case ModeMusicClient:
		os.MkdirAll(f.MusicNode.CacheDir, 0o755)
		f.ClientConf.Name += "-music-client"
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.PingChannel,
			vars.UserChannel,
			vars.MusicChannel,
			vars.MusicPlaylistChannel,
			vars.MusicErrorChannel,
		}
	case ModeMusicDownload:
		os.MkdirAll(f.MusicNode.CacheDir, 0o755)
		f.ClientConf.Name += "-music-download"
		f.ClientConf.History = 0
		f.ClientConf.Channels = []string{
			vars.PingChannel,
			vars.MusicErrorChannel,
			vars.MusicNodeChannel,
			vars.MusicPlaylistSongsChannel,
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
		MusicDownloads:   filepath.Join(f.All.CacheDir, "client-ym"),
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
		ViInsert: "i",
		ViNormal: "escape",

		ViPageDown:    "ctrl-d",
		ViPageUp:      "ctrl-u",
		ViScrollDown:  "j",
		ViScrollUp:    "k",
		ViQuit:        "q",
		ViScrollBegin: "g",
		ViScrollEnd:   "G",

		ViMusicVolumeUp:     "O",
		ViMusicVolumeDown:   "o",
		ViMusicNext:         "n",
		ViMusicPrevious:     "p",
		ViMusicPause:        "space",
		ViMusicSeekForward:  "w",
		ViMusicSeekBackward: "b",
		ViMusicJumpActive:   "a",

		PageDown:    "ctrl-d",
		PageUp:      "ctrl-u",
		ScrollDown:  "ctrl-j",
		ScrollUp:    "ctrl-k",
		ScrollBegin: "ctrl-b",
		ScrollEnd:   "ctrl-e",

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
		MusicJumpActive:         "ctrl-a",
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
