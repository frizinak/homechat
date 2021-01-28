package main

import (
	"fmt"
	"strings"
	"time"
)

type KeyHandler func() bool

type Action string

func (a Action) Mode() Mode {
	if strings.HasPrefix(string(a), "music-") {
		return ModeMusicRemote
	}
	return ModeDefault
}

const (
	ViInsert Action = "vi-mode-insert"
	ViNormal Action = "vi-mode-normal"

	ViPageDown    Action = "vi-page-down"
	ViPageUp      Action = "vi-page-up"
	ViScrollDown  Action = "vi-scroll-down"
	ViScrollUp    Action = "vi-scroll-up"
	ViQuit        Action = "vi-quit"
	ViScrollBegin Action = "vi-scroll-to-top"
	ViScrollEnd   Action = "vi-scroll-to-bottom"

	ViMusicVolumeUp     Action = "vi-music-volume-up"
	ViMusicVolumeDown   Action = "vi-music-volume-down"
	ViMusicNext         Action = "vi-music-next"
	ViMusicPrevious     Action = "vi-music-previous"
	ViMusicPause        Action = "vi-music-pause"
	ViMusicSeekForward  Action = "vi-music-seek-forward"
	ViMusicSeekBackward Action = "vi-music-seek-backward"
	ViMusicJumpActive   Action = "vi-music-jump-to-active"

	PageDown    Action = "page-down"
	PageUp      Action = "page-up"
	ScrollDown  Action = "scroll-down"
	ScrollUp    Action = "scroll-up"
	Backspace   Action = "backspace"
	Completion  Action = "complete"
	Quit        Action = "quit"
	ClearInput  Action = "clear-input"
	Submit      Action = "submit"
	InputDown   Action = "input-down"
	InputUp     Action = "input-up"
	ScrollBegin Action = "scroll-to-top"
	ScrollEnd   Action = "scroll-to-bottom"

	MusicPlaylistCompletion Action = "music-playlist-complete"
	MusicVolumeUp           Action = "music-volume-up"
	MusicVolumeDown         Action = "music-volume-down"
	MusicNext               Action = "music-next"
	MusicPrevious           Action = "music-previous"
	MusicPause              Action = "music-pause"
	MusicSeekForward        Action = "music-seek-forward"
	MusicSeekBackward       Action = "music-seek-backward"
	MusicJumpActive         Action = "music-jump-to-active"
)

type key struct {
	v       byte
	escaped bool
	empty   bool
}

func newKey(k string) (keys []key, err error) {
	p := strings.Split(k, "-")
	add := func(v byte, escaped, empty bool) {
		keys = append(keys, key{v, escaped, empty})
	}
	switch len(p) {
	case 1:
		switch {
		case len(p[0]) == 1:
			add(p[0][0], false, true)
		case p[0] == "space":
			add(' ', false, true)
		case p[0] == "esc" || p[0] == "escape":
			add(27, false, false)
		case p[0] == "tab":
			add(9, false, false)
		case p[0] == "enter" || p[0] == "return":
			add(13, false, false)
		case p[0] == "backspace" || p[0] == "delete":
			add(8, false, false)
			add(127, false, false)
		case p[0] == "up":
			add(65, true, false)
		case p[0] == "down":
			add(66, true, false)
		case p[0] == "right":
			add(67, true, false)
		case p[0] == "left":
			add(68, true, false)
		default:
			err = fmt.Errorf("%s is an invalid key", k)
		}
	case 2:
		if p[0] != "ctrl" {
			err = fmt.Errorf("%s: can only handle ctrl modifier", k)
			return
		}
		if len(p[1]) != 1 {
			err = fmt.Errorf("%s contains multiple characters", k)
			return
		}
		var _ks []key
		_ks, err = newKey(p[1])
		if err != nil {
			return
		}
		for i := range _ks {
			add(_ks[i].v-96, _ks[i].escaped, false)
		}
	default:
		err = fmt.Errorf("%s is an invalid key", k)
	}

	return
}

func Simple(cb func()) KeyHandler {
	return func() bool {
		cb()
		return false
	}
}

type Keys struct {
	vi            bool
	insert        bool
	funcs         map[Action]KeyHandler
	keymap        map[bool]map[bool]map[Mode]map[byte]Action
	onlyWhenEmpty map[bool]map[Mode]map[byte]bool
	escape        byte
	escapeSince   byte
}

func NewKeys(keyMap map[Action]string, actionMap map[Action]KeyHandler, vi func(insertMode bool)) (*Keys, error) {
	keys := &Keys{
		vi:            true,
		insert:        true,
		funcs:         actionMap,
		keymap:        make(map[bool]map[bool]map[Mode]map[byte]Action),
		onlyWhenEmpty: make(map[bool]map[Mode]map[byte]bool),
	}

	actionMap[ViInsert] = func() bool {
		if keys.insert {
			return true
		}

		vi(true)
		keys.insert = true
		return false
	}

	actionMap[ViNormal] = func() bool {
		if !keys.insert {
			return true
		}

		vi(false)
		keys.insert = false
		return false
	}

	keys.keymap[true] = make(map[bool]map[Mode]map[byte]Action)
	keys.keymap[false] = make(map[bool]map[Mode]map[byte]Action)
	for k := range keys.keymap {
		keys.keymap[k][true] = make(map[Mode]map[byte]Action)
		keys.keymap[k][false] = make(map[Mode]map[byte]Action)
	}
	keys.onlyWhenEmpty[true] = make(map[Mode]map[byte]bool)
	keys.onlyWhenEmpty[false] = make(map[Mode]map[byte]bool)
	for a, k := range keyMap {
		action := a
		viModeSwitcher := action == ViNormal || action == ViInsert
		if k == "" {
			if viModeSwitcher {
				keys.vi = false
			}
			continue
		}

		rks, err := newKey(k)
		if err != nil {
			return nil, err
		}

		viBinding := strings.HasPrefix(string(action), "vi-")
		if viBinding {
			if _, ok := actionMap[action]; ok && !viModeSwitcher {
				return nil, fmt.Errorf("vi actions must not be mapped: %s", action)
			}

			action = a[3:]
			if _, ok := actionMap[action]; !ok {
				action = a
			}
			for i := range rks {
				rks[i].empty = false
			}
		}

		if action == ViNormal {
			viBinding = false
		} else if action == ViInsert {
			viBinding = true
		}

		mode := action.Mode()
		if _, ok := actionMap[action]; !ok {
			return nil, fmt.Errorf("key mapped to missing action: %s: %s", k, action)
		}

		for _, rk := range rks {
			if _, ok := keys.keymap[viBinding][rk.escaped][mode]; !ok {
				keys.keymap[viBinding][rk.escaped][mode] = make(map[byte]Action)
				if !viBinding {
					keys.onlyWhenEmpty[rk.escaped][mode] = make(map[byte]bool)
				}
			}
			if _, ok := keys.keymap[viBinding][rk.escaped][mode][rk.v]; ok {
				return keys, fmt.Errorf("duplicate mapping for key %s", k)
			}
			keys.keymap[viBinding][rk.escaped][mode][rk.v] = action
			if !viBinding {
				keys.onlyWhenEmpty[rk.escaped][mode][rk.v] = rk.empty
			}
		}
	}

	return keys, nil
}

func (k *Keys) Do(mode Mode, n byte, empty bool) bool {
	print := true
	pr := func() bool {
		return print && (!k.vi || (k.vi && k.insert))
	}

	switch n {
	case 27:
		k.escape = 1
		k.escapeSince++
		escapeSince := k.escapeSince
		go func() {
			time.Sleep(time.Millisecond * 100)
			if k.escapeSince != escapeSince || k.escape != 1 {
				return
			}
			k.escape = 0
			if ok, _ := k.do(mode, 27, empty); ok || mode == ModeDefault {
				return
			}
			k.do(ModeDefault, 27, empty)
		}()
		print = false
	case 91:
		if k.escape == 1 {
			k.escape = 2
			print = false
		}
	default:
		if k.escape == 2 {
			k.escape = 3
			print = false
			break
		}
		k.escape = 0
	}

	if k.escape != 0 && k.escape != 3 {
		return pr()
	}

	var ok bool
	ok, print = k.do(mode, n, empty)
	if ok || mode == ModeDefault {
		return pr()
	}

	_, print = k.do(ModeDefault, n, empty)
	return pr()
}

func (k *Keys) do(mode Mode, n byte, empty bool) (bool, bool) {
	normal := k.vi && !k.insert
	if !empty && !normal {
		em := k.onlyWhenEmpty[k.escape == 3]
		if emptymap, ok := em[mode]; ok && emptymap[n] {
			return true, true
		}
	}

	m := k.keymap[normal][k.escape == 3]
	if modemap, ok := m[mode]; ok {
		if a, ok := modemap[n]; ok {
			return true, k.funcs[a]()
		}
	}

	return false, k.escape != 3
}
