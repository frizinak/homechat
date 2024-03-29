package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/frizinak/homechat/bytes"
	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/backend/tcp"
	"github.com/frizinak/homechat/client/handler"
	"github.com/frizinak/homechat/client/handler/music"
	musicclient "github.com/frizinak/homechat/client/handler/music/client"
	musicnode "github.com/frizinak/homechat/client/handler/music/node"
	"github.com/frizinak/homechat/client/handler/terminal"
	"github.com/frizinak/homechat/str"
	"github.com/frizinak/homechat/ui"
	"github.com/frizinak/homechat/vars"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"

	"github.com/containerd/console"
)

var onExits []func()

const musicClientAddr = "127.0.0.1:58336"

func upload(f *Flags, backend client.Backend) error {
	args := f.CurrentFlag.Args()
	if len(args) == 0 || args[0] == "" {
		return errors.New("no file specified (reading stdin disabled for now).")
	} else if len(args) > 1 {
		return errors.New("only one file should be passed.")
	}
	file := args[0]

	r, err := os.Open(file)
	if err != nil {
		return err
	}
	defer r.Close()

	stat, err := r.Stat()
	if err != nil {
		return err
	}

	log := ui.Plain(ioutil.Discard)
	handler := terminal.New(log, handler.NoopHandler{})
	cl := client.New(backend, handler, log, f.ClientConf)
	defer cl.Close()

	return cl.Upload(vars.UploadChannel, file, f.Upload.Msg, stat.Size(), r)
}

func update(f *Flags, backend client.Backend) error {
	output := f.Update.Path
	if output == "" {
		var err error
		output, err = os.Executable()
		if err != nil {
			return err
		}
		for {
			stat, err := os.Lstat(output)
			if err != nil {
				return err
			}

			if stat.Mode()&os.ModeSymlink == 0 {
				break
			}

			link, err := os.Readlink(output)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				output = link
				continue
			}

			output = filepath.Clean(filepath.Join(filepath.Dir(output), link))
		}

		if output, err = filepath.Abs(output); err != nil {
			return err
		}
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	rnd := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, rnd)
	if err != nil {
		return err
	}

	tmp := fmt.Sprintf(
		"%s.%s-%s.tmp",
		output,
		stamp,
		base64.RawURLEncoding.EncodeToString(rnd),
	)

	w, err := os.Create(tmp)
	if err != nil {
		return err
	}

	log := ui.Plain(os.Stdout)
	conf := f.MusicNodeConfig
	conf.CustomError = music.NewErrorFlasher(log)
	rhandler := handler.NoopHandler{}
	cl := &client.Client{}
	updateHandler := handler.NewUpdateHandler(rhandler, log, cl)
	*cl = *client.New(backend, updateHandler, log, f.ClientConf)

	defer cl.Close()
	go cl.Run()
	if err := updateHandler.Download(runtime.GOOS, runtime.GOARCH, w); err != nil {
		w.Close()
		os.Remove(tmp)
		return err
	}

	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	var gerr error
	rename := func() bool {
		if err := os.Rename(tmp, output); err != nil {
			gerr = err
			return false
		}
		if err := os.Chmod(output, 0o755); err != nil {
			gerr = err
			return false
		}

		return true
	}

	rename2 := func() bool {
		bak := output + ".bak"
		if err := os.Rename(output, bak); err != nil {
			gerr = err
			return false
		}

		if !rename() {
			os.Rename(bak, output)
			return false
		}
		os.Remove(bak)
		return true
	}

	if rename() || rename2() {
		log.Log("Updated")
		return nil
	}

	os.Remove(tmp)
	return gerr
}

func musicClientCurrent(f *Flags) error {
	n := f.MusicClientCurrent.N
	neverQuit := n == 0

	conn, err := net.Dial("tcp", musicClientAddr)
	if err != nil {
		return err
	}
	scan := bufio.NewScanner(conn)
	scan.Split(bufio.ScanLines)

	for scan.Scan() {
		fmt.Println(scan.Text())
		if neverQuit {
			continue
		}
		n--
		if n <= 0 {
			return nil
		}
	}

	return scan.Err()
}

func musicRemoteCurrent(f *Flags, backend client.Backend) error {
	updates := make(chan client.MusicState, 8)
	handler := music.NewCurrentSongHandler(handler.NoopHandler{}, updates)
	log := ui.Plain(os.Stderr)
	cl := client.New(backend, handler, log, f.ClientConf)
	n := f.MusicRemoteCurrent.N
	neverQuit := n == 0
	first := true

	errs := make(chan error, 1)
	go func() {
		errs <- cl.Run()
	}()

	for {
		select {
		case err := <-errs:
			return err
		case m := <-updates:
			if first {
				first = false
				// current song messages are split into state and song
				// wait until we have both.
				continue
			}
			fmt.Println(m.Format("\t"))
			if neverQuit {
				continue
			}
			n--
			if n <= 0 {
				cl.Close()
				return nil
			}
		}
	}
}

func musicDownload(f *Flags, backend client.Backend) error {
	log := ui.Plain(os.Stdout)
	conf := f.MusicNodeConfig
	conf.CustomError = music.NewErrorFlasher(log)
	di := di.New(conf)
	handler := terminal.New(log, handler.NoopHandler{})
	cl := &client.Client{}
	downloadHandler := music.NewDownloadHandler(handler, log, di.Collection(), cl)
	*cl = *client.New(backend, downloadHandler, log, f.ClientConf)
	go handler.Run(nil)
	go cl.Run()
	defer cl.Close()
	args := strings.Join(f.CurrentFlag.Args(), " ")
	if err := downloadHandler.DownloadPlaylist(args, time.Second*200); err != nil {
		return err
	}
	downloadHandler.Wait()
	return nil
}

func musicInfoFiles(f *Flags) error {
	di := di.New(
		di.Config{
			Log:          log.New(os.Stderr, "", 0),
			StorePath:    f.MusicInfo.Dir,
			AutoSave:     false,
			SimpleOutput: ioutil.Discard,
		},
	)
	col := di.Collection()
	var total uint64
	convert := func(b bytes.Bytes) bytes.Bytes {
		if f.MusicInfoFiles.KiB {
			return b.Convert(bytes.KiB)
		}
		return b.Human()
	}

	for _, path := range col.UnreferencedDownloads() {
		if f.MusicInfoFiles.Stat {
			s, err := os.Stat(path)
			if err != nil {
				fmt.Printf("%10s\t%s\n", "?", path)
				continue
			}
			size := s.Size()
			v := bytes.New(float64(size/1024), bytes.KiB)
			fmt.Printf("%10s\t%s\n", convert(v), path)
			total += uint64(size)
			continue
		}
		fmt.Println(path)
	}

	if f.MusicInfoFiles.Stat {
		v := bytes.New(float64(total/1024), bytes.KiB)
		fmt.Printf("Total: %s\n", convert(v).String())
	}

	return nil
}

func musicInfoDownloads(f *Flags) error {
	di := di.New(
		di.Config{
			Log:          log.New(os.Stderr, "", 0),
			StorePath:    f.MusicInfo.Dir,
			AutoSave:     false,
			SimpleOutput: ioutil.Discard,
		},
	)
	col := di.Collection()
	q := di.Queue()

	all := col.Songs()
	all = append(all, q.Slice()...)
	uniq := make(map[string]struct{}, len(all))
	count := 0
	for _, s := range all {
		gid := collection.GlobalID(s)
		if _, ok := uniq[gid]; ok {
			continue
		}
		uniq[gid] = struct{}{}
		if !s.Local() {
			fmt.Printf("[%s] %s\n", gid, str.StripUnprintable(s.Title()))
			count++
		}
	}

	fmt.Printf("Not downloaded: %d\n", count)
	return nil
}

func musicInfoSongs(f *Flags) error {
	di := di.New(
		di.Config{
			Log:          log.New(os.Stderr, "", 0),
			StorePath:    f.MusicInfo.Dir,
			AutoSave:     false,
			SimpleOutput: ioutil.Discard,
		},
	)

	col := di.Collection()
	q := di.Queue()
	convert := func(b bytes.Bytes) bytes.Bytes {
		if f.MusicInfoSongs.KiB {
			return b.Convert(bytes.KiB)
		}
		return b.Human()
	}

	all := col.Songs()
	all = append(all, q.Slice()...)
	uniq := make(map[string]struct{}, len(all))
	var total uint64
	for _, s := range all {
		gid := collection.GlobalID(s)
		if _, ok := uniq[gid]; ok {
			continue
		}
		uniq[gid] = struct{}{}

		path, err := s.File()
		if err != nil {
			return fmt.Errorf("song error: %s: %s: %w", gid, s.Title(), err)
		}

		title := str.StripUnprintable(s.Title())
		if f.MusicInfoSongs.Stat {
			stat, err := os.Stat(path)
			if err != nil {
				fmt.Printf("%10s\t%-5s\t%-20s\t%s\t%s\n", "?", s.NS(), s.ID(), path, title)
				continue
			}
			size := stat.Size()
			v := bytes.New(float64(size/1024), bytes.KiB)
			fmt.Printf("%10s\t%-5s\t%-20s\t%s\t%s\n", convert(v), s.NS(), s.ID(), path, title)
			total += uint64(size)
			continue
		}

		fmt.Printf("%-5s\t%-20s\t%s\t%s\n", s.NS(), s.ID(), path, title)
	}

	if f.MusicInfoSongs.Stat {
		v := bytes.New(float64(total/1024), bytes.KiB)
		fmt.Printf("Total: %s\n", convert(v).String())
	}

	return nil
}

func oneoff(f *Flags, backend client.Backend) error {
	log := ui.Plain(ioutil.Discard)
	handler := terminal.New(log, handler.NoopHandler{})
	cl := client.New(backend, handler, log, f.ClientConf)
	defer cl.Close()
	var method func(msg string) error
	switch f.All.Mode {
	case ModeMusicRemote, ModeMusicClient:
		method = cl.Music
	default:
		method = cl.Chat
	}

	go cl.Run()
	if f.All.OneOff == "" {
		s := bufio.NewScanner(os.Stdin)
		s.Split(bufio.ScanLines)
		for s.Scan() {
			if err := method(s.Text()); err != nil {
				return err
			}
		}
		return s.Err()
	}

	return method(f.All.OneOff)
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
	sig := make(chan os.Signal, 1)
	fatals := make(chan error)
	exiting := false
	exit := func(err error) {
		if err != nil {
			fatals <- err
			<-make(chan struct{})
		}
	}
	exitClean := func() {
		if exiting {
			return
		}
		exiting = true
		for _, onExit := range onExits {
			onExit()
		}
		os.Exit(0)
	}

	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		var err error
		select {
		case err = <-fatals:
		case <-sig:
		}

		if exiting {
			return
		}
		exiting = true

		msg := "quitting..."
		ex := 0
		if err != nil {
			ex = 1
			msg = err.Error()
		}

		fmt.Fprintln(os.Stderr, msg)
		for _, onExit := range onExits {
			onExit()
		}
		fmt.Fprintln(os.Stderr, "bye")
		os.Exit(ex)
	}()

	interactive := true
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		interactive = false
	}

	var defaultDir string
	if userConfDir, err := os.UserConfigDir(); err == nil {
		defaultDir = filepath.Join(userConfDir, "homechat")
	}

	var defaultCacheDir string
	if userCacheDir, err := os.UserCacheDir(); err == nil {
		defaultCacheDir = filepath.Join(userCacheDir, "homechat")
	}

	f := NewFlags(os.Stdout, defaultDir, defaultCacheDir, interactive)
	f.Flags()
	exit(f.Parse())

	backend := tcp.New(f.TCPConf)
	remoteAddress := f.TCPConf.TCPAddr
	// remoteAddress := f.WSConf.Domain
	// backend, err := ws.New(f.WSConf)
	// exit(err)

	var err error
	simple := true
	switch f.All.Mode {
	case ModeFingerprint:
		err = fingerprint(f, remoteAddress)
	case ModeUpdate:
		err = update(f, backend)
	case ModeUpload:
		err = upload(f, backend)
	case ModeMusicRemoteCurrent:
		err = musicRemoteCurrent(f, backend)
	case ModeMusicClientCurrent:
		err = musicClientCurrent(f)
	case ModeMusicDownload:
		err = musicDownload(f, backend)
	case ModeMusicInfoFiles:
		err = musicInfoFiles(f)
	case ModeMusicInfoDownloads:
		err = musicInfoDownloads(f)
	case ModeMusicInfoSongs:
		err = musicInfoSongs(f)
	default:
		simple = false
	}
	exit(err)
	if simple {
		exitClean()
	}

	if f.All.OneOff != "" || !f.All.Interactive {
		if f.All.Mode != ModeDefault && f.All.Mode != ModeMusicRemote && f.All.Mode != ModeMusicClient {
			exit(errors.New("can not be run non-interactively"))
		}

		exit(oneoff(f, backend))
		exitClean()
	}

	max := f.AppConf.MaxMessages
	if f.All.Mode == ModeMusicRemote || f.All.Mode == ModeMusicNode || f.All.Mode == ModeMusicClient {
		max = 1e9
	}

	tui := ui.Term(
		true,
		max,
		1,
		f.All.Mode == ModeMusicRemote || f.All.Mode == ModeMusicNode || f.All.Mode == ModeMusicClient,
		f.All.UIVisible,
		f.Chat.Zug && f.All.Mode == ModeDefault,
	)

	onExits = append(onExits, func() {
		tui.CursorHide(false)
	})

	handler := terminal.New(tui, handler.NoopHandler{})
	var rhandler client.Handler = handler
	var musicNodeHandler *musicnode.Handler
	var musicClientUI *musicclient.UI
	cl := &client.Client{}
	if f.All.Mode == ModeMusicNode {
		conf := f.MusicNodeConfig
		conf.CustomError = music.NewErrorFlasher(tui)
		di := di.New(conf)
		if _, err := di.BackendAvailable(); err != nil {
			exit(fmt.Errorf("player not available: %w", err))
		}

		player := di.Player()
		onExits = append(onExits, func() {
			player.Close()
		})

		delay := time.Second * 2
		if f.MusicNode.LowLatency {
			delay = time.Millisecond * 50
		}

		musicNodeHandler = musicnode.New(
			cl,
			handler,
			tui,
			delay,
			di.Collection(),
			di.Queue(),
			player,
		)

		rhandler = musicNodeHandler
	} else if f.All.Mode == ModeMusicClient {
		conf := f.MusicNodeConfig
		conf.AutoSave = true
		conf.CustomError = music.NewErrorFlasher(tui)
		conf.Log = log.New(ioutil.Discard, "", 0)
		di := di.New(conf)
		if _, err := di.BackendAvailable(); err != nil {
			exit(fmt.Errorf("player not available: %w", err))
		}

		musicClientUI = musicclient.NewUI(f.MusicClient.Offline, handler, tui, di, cl)
		go func() {
			if err := musicClientUI.ListenAndServe(musicClientAddr); err != nil {
				conf.CustomError.Err(err)
			}
		}()
		rhandler = musicClientUI.Handler()

		onExits = append(onExits, func() {
			musicClientUI.Close()
		})
	}

	*cl = *client.New(backend, rhandler, tui, f.ClientConf)

	typingSig := make(chan struct{}, 1000)
	go func() {
		for range typingSig {
			err := cl.ChatTyping()
			if err != nil {
				tui.Err(err)
			}
		}
	}()

	send := cl.Chat
	typing := func() {
		typingSig <- struct{}{}
	}

	if f.All.Mode != ModeDefault {
		typing = func() {}
	}

	if f.All.Mode == ModeMusicRemote || f.All.Mode == ModeMusicNode {
		send = cl.Music
	} else if f.All.Mode == ModeMusicClient {
		musicClientUI.Input("q")
		send = func(i string) error {
			musicClientUI.Input(i)
			return nil
		}
	}

	if musicClientUI != nil {
		go func() {
			for {
				musicClientUI.Flush()
				time.Sleep(time.Millisecond * 60)
			}
		}()
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
			PageDown:    Simple(tui.ScrollPageDown),
			PageUp:      Simple(tui.ScrollPageUp),
			ScrollDown:  func() bool { tui.Scroll(-1); return false },
			ScrollUp:    func() bool { tui.Scroll(1); return false },
			ScrollBegin: func() bool { tui.Scroll(1<<31 - 1); return false },
			ScrollEnd:   func() bool { tui.Scroll(-1 << 31); return false },
			Backspace:   Simple(tui.BackspaceInput),
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
				tui.Flash("quiting", time.Second*60)
				exitClean()
				return false
			},
			ClearInput: func() bool {
				tui.ResetInput()
				return false
			},
			Submit: func() bool {
				s := string(tui.ResetInput())
				if inputs[len(inputs)-1] != "" {
					inputs = append(inputs, "")
				}
				const max = 100
				if len(inputs) > max {
					inputs = inputs[len(inputs)-max:]
				}
				current = len(inputs) - 1
				if s == "" {
					return false
				}
				if strings.HasPrefix(s, "?") {
					tui.Search(strings.TrimSpace(s[1:]))
					tui.SetInput(s)
					return false
				}
				if strings.HasPrefix(s, "%") {
					u, ok := tui.Link(strings.TrimSpace(s[1:]))
					if !ok {
						return false
					}
					go func() {
						err := f.Opener.OpenURL(u.String())
						if err != nil {
							tui.Flash(fmt.Sprintf("failed to open url: %s", err), 0)
						}
					}()
					return false
				}

				send(s)
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
			InputLeft: func() bool {
				tui.Left()
				return false
			},
			InputRight: func() bool {
				tui.Right()
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
				if musicNodeHandler != nil {
					musicNodeHandler.IncreaseVolume(0.05)
					return false
				}
				send("volume +5")
				return false
			},
			MusicVolumeDown: func() bool {
				if musicNodeHandler != nil {
					musicNodeHandler.IncreaseVolume(-0.05)
					return false
				}
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
				send("p")
				return false
			},
			MusicSeekForward: func() bool {
				send("seek +5")
				return false
			},
			MusicSeekBackward: func() bool {
				send("seek -5")
				return false
			},
			MusicJumpActive: func() bool {
				tui.JumpToActive()
				tui.Flush()
				tui.JumpToActive()
				send("q")

				return false
			},
		},
		func(insertMode bool) {
			tui.CursorHide(!insertMode)
		},
	)
	exit(err)

	if !f.MusicClient.Offline {
		fmt.Fprintf(os.Stderr, "Shaking hands with %s\n", remoteAddress)
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
				exit(errors.New("Not connecting, smart choice!"))
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
	}

	tui.Start()

	exit(currentConsole.SetRaw())
	onExits = append(onExits, func() {
		cl.Close()
		resetTTY()
	})

	input := bufio.NewReader(os.Stdin)
	go func() {
		keymode := f.All.Mode
		if keymode == ModeMusicNode || keymode == ModeMusicClient {
			keymode = ModeMusicRemote
		}
		for {
			n, err := input.ReadByte()
			exit(err)

			input := tui.GetInput()
			if current == len(inputs)-1 || inputs[current] != input {
				inputs[len(inputs)-1] = input
			}

			if keys.Do(keymode, n, input == "") {
				tui.Input(n)
				typing()
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
			rcmd[i] = strings.ReplaceAll(rcmd[i], "%u", str.StripUnprintable(meta))
			rcmd[i] = strings.ReplaceAll(rcmd[i], "%m", str.StripUnprintable(data))
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
	if f.MusicClient.Offline {
		tui.Log("offline")
		<-make(chan struct{}, 0)
	}

	exit(cl.Run())
	exitClean()
}
