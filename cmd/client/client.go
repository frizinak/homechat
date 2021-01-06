package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/tcp"
	"github.com/frizinak/homechat/client/terminal"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/ui"
	"github.com/frizinak/homechat/vars"
	"github.com/google/shlex"

	"github.com/containerd/console"
)

func ensureFile(file string, defaultContents []byte) error {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		f, err = os.Create(file)
		if err != nil {
			return err
		}
		defer f.Close()
		if defaultContents != nil {
			_, err = f.Write(defaultContents)
		}
		return err
	}

	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func exit(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

type mode byte

const (
	modeDefault mode = iota
	modeMusic
	modeUpload
)

func main() {
	isNonInteractive := false
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		isNonInteractive = true
	}

	var defaultDir string
	if home, err := os.UserHomeDir(); err == nil {
		defaultDir = filepath.Join(home, ".config", "homechat")
	}

	var configDir string
	var uploadMsg string
	var uploadFile string

	out := os.Stdout
	chatFlags := flag.NewFlagSet("chat", flag.ExitOnError)
	chatFlags.SetOutput(out)
	musicFlags := flag.NewFlagSet("music", flag.ExitOnError)
	musicFlags.SetOutput(out)
	uploadFlags := flag.NewFlagSet("upload", flag.ExitOnError)
	uploadFlags.SetOutput(out)
	uploadFlags.StringVar(&uploadMsg, "m", "Download: ", "prefix upload url with this message")

	flag.CommandLine.SetOutput(out)
	flag.StringVar(&configDir, "c", defaultDir, "config directory")
	flag.Usage = func() {
		fmt.Fprintln(out, "homechat")
		flag.PrintDefaults()
		fmt.Fprint(out, "\nCommands:\n")
		fmt.Fprintln(out, "  - chat | <empty>: Chat client")
		fmt.Fprintln(out, "  - music:          Music client")
		fmt.Fprintln(out, "  - upload:         Upload a file from stdin or commandline to chat")
		fmt.Fprintln(out, "  - version:        Print version and exit")
	}
	chatFlags.Usage = func() {
		chatFlags.PrintDefaults()
		fmt.Fprintln(out, "Run interactively")
		fmt.Fprintln(out, " - homechat chat")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Send message and exit")
		fmt.Fprintln(out, " - homechat chat <message to send>")
		fmt.Fprintln(out, " - command | homechat chat")
	}
	musicFlags.Usage = func() {
		musicFlags.PrintDefaults()
		fmt.Fprintln(out, "Run interactively")
		fmt.Fprintln(out, " - homechat music")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Send command and exit")
		fmt.Fprintln(out, " - homechat music <command to send>")
		fmt.Fprintln(out, " - command | homechat music")
	}
	uploadFlags.Usage = func() {
		uploadFlags.PrintDefaults()
		fmt.Fprintln(out, "Usage")
		fmt.Fprintln(out, " - homechat upload <filepath>")
		fmt.Fprintln(out, " - command | homechat upload")
	}
	flag.Parse()

	if configDir == "" {
		exit(errors.New("please specify a config directory"))
	}
	os.MkdirAll(configDir, 0755)
	configNotify := filepath.Join(configDir, "notify")
	configUsername := filepath.Join(configDir, "username")
	configServer := filepath.Join(configDir, "server")
	configKeymap := filepath.Join(configDir, "keymap.json")

	defaultKeymap := map[Action]string{
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

		MusicVolumeUp:     "up",
		MusicVolumeDown:   "down",
		MusicNext:         "right",
		MusicPrevious:     "left",
		MusicPause:        "space",
		MusicSeekForward:  "]",
		MusicSeekBackward: "[",
	}

	defaultKeymapJSON, err := json.MarshalIndent(defaultKeymap, "", "    ")
	exit(err)

	exit(ensureFile(configNotify, []byte("notify-send 'HomeChat' '%u: %m'")))
	exit(ensureFile(configUsername, nil))
	exit(ensureFile(configServer, nil))
	exit(ensureFile(configKeymap, defaultKeymapJSON))

	rnotifCmd, err := ioutil.ReadFile(configNotify)
	exit(err)
	rusername, err := ioutil.ReadFile(configUsername)
	exit(err)
	raddress, err := ioutil.ReadFile(configServer)
	exit(err)

	keymap := make(map[Action]string, len(defaultKeymap))
	exit(func() error {
		f, err := os.Open(configKeymap)
		if err != nil {
			return err
		}
		err = json.NewDecoder(f).Decode(&keymap)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to parse keymap ('%s'): %w", configKeymap, err)
		}

		resave := false
		for i := range defaultKeymap {
			if _, ok := keymap[i]; !ok {
				resave = true
				keymap[i] = defaultKeymap[i]
			}
		}
		for i := range keymap {
			if _, ok := defaultKeymap[i]; !ok {
				return fmt.Errorf("unknown action '%s' in keymap", i)
			}
		}

		if resave {
			f, err := os.Create(configKeymap)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(f)
			enc.SetIndent("", "    ")
			err = enc.Encode(keymap)
			f.Close()
			return err
		}
		return nil
	}())

	var c client.Config
	c.Name = strings.TrimSpace(string(rusername))
	if c.Name == "" {
		exit(fmt.Errorf("please specify your desired username in %s", configUsername))
	}
	notifCmd, err := shlex.Split(string(rnotifCmd))
	if err != nil {
		panic(err)
	}

	if len(raddress) == 0 {
		exit(fmt.Errorf("please specify the server ip:port in %s", configServer))
	}

	bc := tcp.Config{Domain: strings.TrimSpace(string(raddress))}
	c.Framed = false
	c.Proto = channel.ProtoBinary
	c.ServerURL = "http://" + bc.Domain

	var mode mode
	var oneOff string
	args := flag.Args()
	switch flag.Arg(0) {
	case "", "chat":
		mode = modeDefault
		if len(args) == 0 {
			break
		}
		exit(chatFlags.Parse(args[1:]))
		oneOff = strings.Join(chatFlags.Args(), " ")
	case "music":
		mode = modeMusic
		exit(musicFlags.Parse(args[1:]))
		oneOff = strings.Join(musicFlags.Args(), " ")
	case "upload":
		mode = modeUpload
		exit(uploadFlags.Parse(args[1:]))
		uploadFile = uploadFlags.Arg(0)
		if uploadFile == "" && !isNonInteractive {
			exit(errors.New("please provide a file"))
		}
	case "version":
		fmt.Fprintf(out, "%s (protocol: %s)\n", vars.Version, vars.ProtocolVersion)
		os.Exit(0)
	default:
		flag.Usage()
		os.Exit(1)
	}

	c.Channels = []string{vars.UserChannel}
	c.History = true
	ch := vars.ChatChannel
	if mode == modeMusic {
		c.History = false
		if isNonInteractive {
			exit(errors.New("music can only be used with an interactive terminal"))
		}
		ch = vars.MusicChannel
		c.Channels = append(c.Channels, vars.MusicStateChannel)
		c.Channels = append(c.Channels, vars.MusicErrorChannel)
	}
	c.Channels = append(c.Channels, ch)

	if mode == modeUpload {
		c.History = false
		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)

		c.Channels = []string{}
		tcp, err := tcp.New(bc)
		exit(err)
		client := client.New(tcp, handler, log, c)
		var r io.ReadCloser = os.Stdin
		if uploadFile != "" {
			r, err = os.Open(uploadFile)
			exit(err)
		}
		err = client.Upload(vars.UploadChannel, uploadFile, uploadMsg, r)
		r.Close()
		exit(err)
		os.Exit(0)
	}

	if oneOff != "" || isNonInteractive {
		c.History = false
		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)
		c.Channels = []string{}
		tcp, err := tcp.New(bc)
		exit(err)
		client := client.New(tcp, handler, log, c)
		if oneOff == "" {
			r := io.LimitReader(os.Stdin, 1024*1024)
			d, err := ioutil.ReadAll(r)
			exit(err)
			oneOff = string(d)
		}

		if mode == modeMusic {
			exit(client.Music(oneOff))
			os.Exit(0)
		}
		exit(client.Chat(oneOff))
		os.Exit(0)
	}

	indent := 1
	if mode == modeMusic {
		indent = 2
	}
	tui := ui.Term(mode == modeDefault, indent, mode == modeMusic)
	handler := terminal.New(tui)
	tcp, err := tcp.New(bc)
	//ws, err := ws.New(ws.Config{TLS: false, Domain: bc.Domain, Path: "ws"})
	exit(err)
	client := client.New(tcp, handler, tui, c)
	send := func(msg string) error { return client.Chat(msg) }
	if mode == modeMusic {
		send = func(msg string) error { return client.Music(msg) }
	}

	go func() {
		for {
			tui.Flush()
			time.Sleep(time.Second)
		}
	}()

	currentConsole := console.Current()
	resetTTY := func() {
		currentConsole.Reset()
	}

	inputs := make([]string, 1)
	current := 0
	keys, err := NewKeys(
		keymap,
		map[Action]KeyHandler{
			PageDown:   Simple(tui.ScrollPageDown),
			PageUp:     Simple(tui.ScrollPageUp),
			ScrollDown: func() bool { tui.Scroll(-1); return false },
			ScrollUp:   func() bool { tui.Scroll(1); return false },
			Backspace:  Simple(tui.BackspaceInput),
			Completion: func() bool {
				cur := tui.GetInput()
				if len(cur) == 0 {
					return false
				}

				p := strings.Split(cur, " ")
				l := p[len(p)-1]
				if l[0] != '@' {
					return false
				}

				found := ""
				foundC := 0
				name := client.Name()
				for _, n := range client.Users() {
					if n.Name == name {
						continue
					}

					if strings.HasPrefix(n.Name, l[1:]) {
						found = n.Name
						foundC++
					}
				}

				if foundC == 1 {
					i := fmt.Sprintf(
						"@%s ",
						found,
					)
					if len(p) > 1 {
						i = fmt.Sprintf(
							"%s %s",
							strings.Join(p[:len(p)-1], " "),
							i,
						)
					}

					tui.SetInput(i)
				}

				return false
			},
			Quit: func() bool {
				resetTTY()
				os.Exit(0)
				return false
			},
			ClearInput: func() bool {
				tui.ResetInput()
				return false
			},
			Submit: func() bool {
				s := tui.ResetInput()
				cmd := strings.TrimSpace(string(s))
				inputs = append(inputs, "")
				const max = 30
				if len(inputs) > max {
					inputs = inputs[len(inputs)-max:]
				}
				current = len(inputs) - 1
				send(cmd)
				return false
			},
			InputDown: func() bool {
				current++
				if len(inputs) > current {
					tui.SetInput(inputs[current])
					return false
				}
				current--
				tui.Flush()
				return false
			},
			InputUp: func() bool {
				current--
				if current < 0 {
					current = 0
				}
				i := ""
				if len(inputs) > current {
					i = inputs[current]
				}
				tui.SetInput(i)
				return false
			},
			MusicVolumeUp: func() bool {
				send("volume +5")
				return false
			},
			MusicVolumeDown: func() bool {
				send("volume -5")
				return false
			},
			MusicNext: func() bool {
				send("next")
				return false
			},
			MusicPrevious: func() bool {
				send("prev")
				return false
			},
			MusicPause: func() bool {
				if strings.TrimSpace(tui.GetInput()) == "" {
					send("p")
					return false
				}
				return true
			},
			MusicSeekForward: func() bool {
				if strings.TrimSpace(tui.GetInput()) == "" {
					send("seek 5")
					return false
				}

				return true
			},
			MusicSeekBackward: func() bool {
				if strings.TrimSpace(tui.GetInput()) == "" {
					send("seek -5")
					return false
				}

				return true
			},
		},
	)

	if err != nil {
		panic(err)
	}

	exit(currentConsole.SetRaw())
	input := bufio.NewReader(os.Stdin)
	go func() {
		for {
			n, err := input.ReadByte()
			exit(err)

			input := tui.GetInput()
			if current == len(inputs)-1 || inputs[current] != input {
				inputs[len(inputs)-1] = input
			}

			if keys.Do(mode, n) {
				tui.Input(n)
			}
		}
	}()

	notify := func(msg ui.Msg) {
		if len(notifCmd) == 0 {
			return
		}
		meta := msg.From
		data := msg.Message
		rcmd := make([]string, len(notifCmd))
		copy(rcmd, notifCmd)
		for i := range rcmd {
			rcmd[i] = strings.ReplaceAll(rcmd[i], "%u", meta)
			rcmd[i] = strings.ReplaceAll(rcmd[i], "%m", data)
		}

		cmd := exec.Command(rcmd[0], rcmd[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}

	msgs := make(chan ui.Msg, 100)
	go func() {
		var lmsg *ui.Msg
		after := time.After(time.Millisecond)
		for {
			select {
			case msg := <-msgs:
				if c.Name != msg.From && msg.Notify {
					lmsg = &msg
				}
			case <-after:
				if lmsg == nil {
					after = time.After(time.Millisecond * 500)
					continue
				}
				after = time.After(time.Second * 5)
				go notify(*lmsg)
				lmsg = nil
			}
		}
	}()

	go handler.Run(msgs)
	err = client.Run()
	resetTTY()
	exit(err)
}
