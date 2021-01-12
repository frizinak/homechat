package main

import (
	"bufio"
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
	"github.com/frizinak/homechat/ui"
	"github.com/frizinak/homechat/vars"

	"github.com/containerd/console"
)

func exit(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	interactive := true
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		interactive = false
	}

	var defaultDir string
	if userConfDir, err := os.UserConfigDir(); err == nil {
		defaultDir = filepath.Join(userConfDir, "homechat")
	}

	f := NewFlags(os.Stdout, defaultDir, interactive)
	f.Flags()
	exit(f.Parse())

	tcp := tcp.New(f.TCPConf)
	if f.All.Mode == ModeUpload {
		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)
		client := client.New(tcp, handler, log, f.ClientConf)
		var r io.ReadCloser = os.Stdin
		if f.Upload.File != "" {
			var err error
			r, err = os.Open(f.Upload.File)
			exit(err)
		}
		err := client.Upload(vars.UploadChannel, f.Upload.File, f.Upload.Msg, r)
		r.Close()
		exit(err)
		os.Exit(0)
	}

	if f.All.OneOff != "" || !f.All.Interactive {
		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)
		client := client.New(tcp, handler, log, f.ClientConf)
		if f.All.OneOff == "" {
			r := io.LimitReader(os.Stdin, 1024*1024)
			if f.All.Linemode {
				s := bufio.NewScanner(r)
				s.Split(bufio.ScanLines)
				for s.Scan() {
					exit(client.Chat(s.Text()))
				}
				exit(s.Err())
				os.Exit(0)
			}

			d, err := ioutil.ReadAll(r)
			exit(err)
			f.All.OneOff = string(d)
		}

		if f.All.Mode == ModeMusic {
			exit(client.Music(f.All.OneOff))
			os.Exit(0)
		}
		exit(client.Chat(f.All.OneOff))
		os.Exit(0)
	}

	indent := 1
	if f.All.Mode == ModeMusic {
		indent = 2
	}
	max := f.AppConf.MaxMessages
	if f.All.Mode == ModeMusic {
		max = 1e9
	}

	tui := ui.Term(
		f.All.Mode == ModeDefault,
		max,
		indent,
		f.All.Mode == ModeMusic,
	)
	handler := terminal.New(tui)
	client := client.New(tcp, handler, tui, f.ClientConf)
	send := client.Chat
	if f.All.Mode == ModeMusic {
		send = client.Music
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
		f.Keymap,
		map[Action]KeyHandler{
			PageDown:   Simple(tui.ScrollPageDown),
			PageUp:     Simple(tui.ScrollPageUp),
			ScrollDown: func() bool { tui.Scroll(-1); return false },
			ScrollUp:   func() bool { tui.Scroll(1); return false },
			Backspace:  Simple(tui.BackspaceInput),
			Completion: func() bool {
				n := complete(
					tui.GetInput(),
					"@",
					client.Users().Names(),
					map[string]struct{}{client.Name(): {}},
				)
				if n != "" {
					tui.SetInput(n)
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
			MusicPlaylistCompletion: func() bool {
				n := complete(
					tui.GetInput(),
					"",
					client.Playlists(),
					nil,
				)
				if n != "" {
					tui.SetInput(n)
				}
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
					send("seek +5")
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
	exit(err)

	exit(client.Connect())
	tui.Start()

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

			if keys.Do(f.All.Mode, n) {
				tui.Input(n)
			}
		}
	}()

	notify := func(msg ui.Msg) {
		if len(f.Chat.NotifyCommand) == 0 {
			return
		}
		meta := msg.From
		data := msg.Message
		rcmd := make([]string, len(f.Chat.NotifyCommand))
		copy(rcmd, f.Chat.NotifyCommand)
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
				if f.ClientConf.Name == msg.From || msg.NotifyNever() {
					break
				}

				if f.AppConf.NotifyWhen == NotifyDefault && !msg.NotifyPersonal() {
					break
				}

				lmsg = &msg
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
