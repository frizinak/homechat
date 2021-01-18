package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/backend/tcp"
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

func fingerprint(f *Flags, remoteAddress string) error {
	pk, err := f.All.Key.Public()
	if err != nil {
		return fmt.Errorf("failed to parse publickey: %w", err)
	}

	serverFP := f.AppConf.ServerFingerprint
	if serverFP == "" {
		serverFP = "<none>"
	}

	fmt.Printf(
		"%-30s\t%s\n%-30s\t%s\n",
		"local",
		pk.FingerprintString(),
		fmt.Sprintf("remote[%s]", remoteAddress),
		serverFP,
	)

	return nil
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

	remoteAddress := f.TCPConf.TCPAddr
	// remoteAddress := f.WSConf.Domain

	var err error
	switch f.All.Mode {
	case ModeFingerprint:
		exit(fingerprint(f, remoteAddress))
		os.Exit(0)
	}

	backend := tcp.New(f.TCPConf)
	// backend, err := ws.New(f.WSConf)
	// exit(err)

	if f.All.Mode == ModeUpload {
		if f.Upload.File == "" && interactive {
			exit(errors.New("no file specified"))
		}

		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)
		cl := client.New(backend, handler, log, f.ClientConf)
		defer cl.Close()

		if f.Upload.File == "" {
			// broken atm as os.Stdin is not seekable: todo: copy to temp file
			err := cl.Upload(vars.UploadChannel, f.Upload.File, f.Upload.Msg, os.Stdin)
			exit(err)
			return
		}

		r, err := os.Open(f.Upload.File)
		if err != nil {
			exit(err)
		}
		defer r.Close()

		var size int64
		if stat, err := r.Stat(); err == nil {
			size = stat.Size()
		}

		if size != 0 {
			exit(cl.UploadSize(vars.UploadChannel, f.Upload.File, f.Upload.Msg, size, r))
			return
		}

		exit(cl.Upload(vars.UploadChannel, f.Upload.File, f.Upload.Msg, r))
		return
	}

	if f.All.OneOff != "" || !f.All.Interactive {
		log := ui.Plain(ioutil.Discard)
		handler := terminal.New(log)
		cl := client.New(backend, handler, log, f.ClientConf)
		defer cl.Close()
		if f.All.OneOff == "" {
			r := io.LimitReader(os.Stdin, 1024*1024)
			if f.All.Linemode {
				s := bufio.NewScanner(r)
				s.Split(bufio.ScanLines)
				for s.Scan() {
					exit(cl.Chat(s.Text()))
				}
				exit(s.Err())
				return
			}

			d, err := ioutil.ReadAll(r)
			exit(err)
			f.All.OneOff = string(d)
		}

		if f.All.Mode == ModeMusic {
			exit(cl.Music(f.All.OneOff))
			return
		}
		exit(cl.Chat(f.All.OneOff))
		return
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
	cl := client.New(backend, handler, tui, f.ClientConf)
	closing := false
	send := cl.Chat
	if f.All.Mode == ModeMusic {
		send = cl.Music
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
					cl.Users().Names(),
					map[string]struct{}{cl.Name(): {}},
				)
				if n != "" {
					tui.SetInput(n)
				}
				return false
			},
			Quit: func() bool {
				if closing {
					return false
				}
				closing = true
				cl.Close()
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
					cl.Playlists(),
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

	fmt.Printf("Shaking hands with %s\n", remoteAddress)
	err = cl.Connect()
	if err == client.ErrFingerPrint {
		trust := f.AppConf.ServerFingerprint
		newFP := cl.ServerFingerprint()
		if newFP == "" {
			exit(errors.New("Something went wrong during authentication"))
		}
		msg := "Server fingerprint changed!\nDo not blindly accept as something malicious might be going on."
		if trust == "" {
			msg = "Connecting to new server for first time.\nAsk the administrator of the server if the following key is correct:"
		}

		fmt.Fprintf(
			os.Stderr,
			"%s\n%s\nAccept new fingerprint for %s? [y/N]: ",
			msg,
			newFP,
			remoteAddress,
		)
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Fprintln(os.Stderr, "Not connecting, smart choice!")
			os.Exit(1)
		}

		f.AppConf.ServerFingerprint = newFP
		exit(f.SaveConfig())
		cl.SetTrustedFingerprint(newFP)
		err = cl.Connect()
		if err == client.ErrFingerPrint {
			fmt.Fprintln(os.Stderr, "Server fingerprint changed AGAIN!")
			fmt.Fprintln(os.Stderr, "Not connecting, try again.")
		}
	}
	exit(err)
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
	err = cl.Run()
	resetTTY()
	exit(err)
}
