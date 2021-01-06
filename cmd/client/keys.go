package main

import (
	"fmt"
	"strings"
)

type KeyConfig struct {
}

type KeyHandler func() bool

type Action string

func (a Action) Mode() mode {
	if strings.HasPrefix(string(a), "music-") {
		return modeMusic
	}
	return modeDefault
}

const (
	PageDown   Action = "page-down"
	PageUp     Action = "page-up"
	ScrollDown Action = "scroll-down"
	ScrollUp   Action = "scroll-up"
	Backspace  Action = "backspace"
	Completion Action = "complete"
	Quit       Action = "quit"
	ClearInput Action = "clear-input"
	Submit     Action = "submit"
	InputDown  Action = "input-down"
	InputUp    Action = "input-up"

	MusicVolumeUp     Action = "music-volume-up"
	MusicVolumeDown   Action = "music-volume-down"
	MusicNext         Action = "music-next"
	MusicPrevious     Action = "music-previous"
	MusicPause        Action = "music-pause"
	MusicSeekForward  Action = "music-seek-forward"
	MusicSeekBackward Action = "music-seek-backward"
)

type key struct {
	v       byte
	escaped bool
}

func newKey(k string) (keys []key, err error) {
	p := strings.Split(k, "-")
	add := func(v byte, escaped bool) {
		keys = append(keys, key{v, escaped})
	}
	switch len(p) {
	case 1:
		switch {
		case len(p[0]) == 1:
			add(p[0][0], false)
		case p[0] == "space":
			add(' ', false)
		case p[0] == "tab":
			add(9, false)
		case p[0] == "enter" || p[0] == "return":
			add(13, false)
		case p[0] == "backspace" || p[0] == "delete":
			add(8, false)
			add(127, false)
		case p[0] == "up":
			add(65, true)
		case p[0] == "down":
			add(66, true)
		case p[0] == "right":
			add(67, true)
		case p[0] == "left":
			add(68, true)
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
			add(_ks[i].v-96, _ks[i].escaped)
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
	funcs  map[Action]KeyHandler
	keymap map[bool]map[mode]map[byte]Action
	escape byte
}

func NewKeys(keyMap map[Action]string, actionMap map[Action]KeyHandler) (*Keys, error) {
	keys := &Keys{funcs: actionMap, keymap: make(map[bool]map[mode]map[byte]Action)}

	keys.keymap[true] = make(map[mode]map[byte]Action)
	keys.keymap[false] = make(map[mode]map[byte]Action)
	for a, k := range keyMap {
		if _, ok := actionMap[a]; !ok {
			return nil, fmt.Errorf("key mapped to missing action: %s: %s", k, a)
		}

		rks, err := newKey(k)
		if err != nil {
			return nil, err
		}
		mode := a.Mode()

		for _, rk := range rks {
			if _, ok := keys.keymap[rk.escaped][mode]; !ok {
				keys.keymap[rk.escaped][mode] = make(map[byte]Action)
			}
			if _, ok := keys.keymap[rk.escaped][mode][rk.v]; ok {
				return keys, fmt.Errorf("duplicate mapping for key %s", k)
			}
			keys.keymap[rk.escaped][mode][rk.v] = a
		}
	}

	return keys, nil
}

func (k *Keys) Do(mod mode, n byte) (print bool) {
	// debug, err := os.OpenFile("/tmp/debug", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	// if err != nil {
	// 	panic(err)
	// }
	// defer debug.Close()

	print = true
	switch n {
	case 27:
		k.escape = 1
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

	// _, err = fmt.Fprintln(debug, mod, n, k.escape)
	// if err != nil {
	// 	panic(err)
	// }

	if k.escape != 0 && k.escape != 3 {
		return
	}

	var ok bool
	ok, print = k.do(mod, n)
	if ok || mod == modeDefault {
		return
	}

	_, print = k.do(modeDefault, n)
	return print
}

func (k *Keys) do(mode mode, n byte) (bool, bool) {
	m := k.keymap[k.escape == 3]
	if modemap, ok := m[mode]; ok {
		if a, ok := modemap[n]; ok {
			return true, k.funcs[a]()
		}
	}

	return false, k.escape != 3
}
