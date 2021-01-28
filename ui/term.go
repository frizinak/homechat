package ui

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/console"
	"github.com/mattn/go-runewidth"
)

const stampFormat = "01-02 15:04"

type TermUI struct {
	metaPrefix bool
	indent     int
	scrollTop  bool

	status      string
	flash       string
	flashExpiry time.Time

	latency *time.Duration

	sem   sync.Mutex
	log   []msg
	input []byte
	users []string

	scrollPage           int
	scrollSimple         int
	scroll               int
	jumpToActive         bool
	jumpToQuery          string
	jumpToQueryCount     uint16
	jumpToQueryCountLast uint16

	maxMessages int

	cursorHide        bool
	cursorHiddenState bool

	s State

	disabled bool
}

type msg struct {
	prefix    string
	msg       string
	highlight Highlight
}

func Term(metaPrefix bool, maxMessages, indent int, scrollTop bool) *TermUI {
	return &TermUI{
		metaPrefix:  metaPrefix,
		indent:      indent,
		scrollTop:   scrollTop,
		maxMessages: maxMessages,
		disabled:    true,

		jumpToQueryCountLast: 1<<16 - 1,
	}
}

func (ui *TermUI) Start() {
	ui.disabled = false
	ui.Flush()
}

func (ui *TermUI) Users(msg string) {
	ui.users = strings.Split(msg, "\n")
	ui.Flush()
}

func (ui *TermUI) Latency(n time.Duration) {
	ui.latency = &n
	ui.Flush()
}

func (ui *TermUI) Flash(msg string, dur time.Duration) {
	if dur == 0 {
		dur = time.Second * 5
	}
	ui.flashExpiry = time.Now().Add(dur)
	ui.flash = msg
	ui.Flush()
}

func (ui *TermUI) Log(msg string)    { ui.status = msg; ui.Flush() }
func (ui *TermUI) ErrStr(err string) { ui.status = err; ui.Flush() }
func (ui *TermUI) Err(err error)     { ui.ErrStr(err.Error()) }

func (ui *TermUI) Clear() {
	ui.sem.Lock()
	ui.log = make([]msg, 0)
	ui.sem.Unlock()
}

func (ui *TermUI) JumpToActive() { ui.jumpToActive = true }
func (ui *TermUI) Search(qry string) {
	qry = strings.ToLower(qry)
	ui.sem.Lock()
	same := ui.jumpToQuery == qry
	ui.jumpToQuery = qry
	ui.jumpToQueryCount++
	if !same {
		ui.jumpToQueryCount = 0
	}

	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) Broadcast(msgs []Msg, scroll bool) {
	ui.sem.Lock()
	for _, m := range msgs {
		texts := strings.Split(strings.ReplaceAll(m.Message, "\r", ""), "\n")
		for _, text := range texts {
			msg := msg{"", text, m.Highlight}
			if ui.metaPrefix {
				msg.prefix = fmt.Sprintf(
					"%s %s",
					m.Stamp.Format(stampFormat),
					m.From,
				)
				msg.prefix = runewidth.FillRight(msg.prefix, len(stampFormat)+15+1) + "│ "
			}
			ui.log = append(ui.log, msg)
		}
	}

	if len(ui.log) > ui.maxMessages {
		ui.log = ui.log[len(ui.log)-ui.maxMessages:]
	}
	if ui.scrollTop && scroll {
		ui.scroll = math.MaxInt32
	}
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) MusicState(s State) {
	if s == ui.s {
		return
	}

	ui.s = s
	ui.Flush()
}

func (ui *TermUI) ScrollPageUp() {
	ui.sem.Lock()
	ui.scrollPage++
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) ScrollPageDown() {
	ui.sem.Lock()
	ui.scrollPage--
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) Scroll(amount int) {
	ui.sem.Lock()
	ui.scrollSimple += amount
	ui.sem.Unlock()
	ui.Flush()
}

var (
	cursorTop    = []byte("\033[H")
	cursorHide   = []byte("\033[?25l")
	cursorShow   = []byte("\033[?25h")
	clear        = []byte("\033[H\033[J")
	clrLine      = []byte("\033[1m")
	clrStatus    = []byte("\033[40;37m")
	clrUser      = []byte("\033[40;37m")
	clrReset     = []byte("\033[0m")
	clrMusicSeek = []byte("\033[1;32m")
	clrMusicIcon = []byte("\033[1;32m")
)

var hl = map[Highlight][]byte{
	HLTitle:     []byte("\033[1m"),
	HLActive:    []byte("\033[1;37;41m"),
	HLMuted:     []byte("\033[40;37m"),
	HLSlight:    []byte("\033[1m"),
	HLProblem:   []byte("\033[1;31m"),
	HLTemporary: []byte("\033[1;32m"),
}

const (
	chrPlaying = "\u25B8" //"\u25BA" //"\u22D7" //"\u2023" //"‣▶"
	chrPause   = "\u2016" //"\u2225" // ⏸"
	chrBar     = "\u23AF" //"\u22EF"
	chrLine    = "\u2E3B"
)

func (ui *TermUI) Flush() {
	if ui.disabled {
		return
	}

	ui.sem.Lock()
	defer ui.sem.Unlock()

	w, h := 50, 10
	if size, err := console.Current().Size(); err == nil {
		w, h = int(size.Width), int(size.Height)
	}

	h -= 5
	state := ui.s
	if state.Song != "" {
		h -= 3
	}

	ui.scroll += ui.scrollPage * h / 2
	ui.scroll += ui.scrollSimple
	ui.scrollPage = 0
	ui.scrollSimple = 0
	if ui.scroll < 0 {
		ui.scroll = 0
	}

	if w < 50 {
		w = 50
	}
	rw := w
	w -= ui.indent

	nmsgs := h
	if nmsgs < 0 {
		nmsgs = 0
	}

	pad := func(n, padchr string, total int) string {
		width := runewidth.StringWidth(n)
		padwidth := runewidth.StringWidth(padchr)
		count := total - width
		rcount := count / padwidth
		padbyt := []byte(padchr)

		if rcount <= 0 {
			return n
		}

		b := make([]byte, 0, rcount*len(padbyt))
		for i := 0; i < rcount; i++ {
			b = append(b, padbyt...)
		}
		return n + string(b)
	}

	suffpref := func(prefix, suffix, str string) string {
		if prefix == "" && suffix == "" {
			return str
		}
		str = pad(str, " ", w)
		str = prefix + str
		str = str + suffix
		return str
	}

	logs := make([]string, 0, len(ui.log))
	scrollMsg := -1
	search := ""
	var searchMatches uint16
	if ui.jumpToQueryCountLast != ui.jumpToQueryCount && ui.jumpToQuery != "" {
		search = ui.jumpToQuery
		ui.jumpToQueryCountLast = ui.jumpToQueryCount
	}

	for i := 0; i < len(ui.log); i++ {
		meta := ui.log[i].prefix
		log := ui.log[i].msg
		both := meta + log

		ln := runewidth.StringWidth(both)

		if ui.jumpToActive && ui.log[i].highlight&HLActive != 0 {
			ui.jumpToActive = false
			scrollMsg = len(logs) + 1
		}

		if search != "" {
			ui.log[i].highlight &= ^HLTemporary
			if strings.Contains(strings.ToLower(log), search) {
				if searchMatches == ui.jumpToQueryCount {
					scrollMsg = len(logs) + 1
					search = ""
					ui.log[i].highlight |= HLTemporary
				}
				searchMatches++
			}
		}

		var prefix, suffix string
		if ui.log[i].highlight > 0 {
			for v := range hl {
				if v&ui.log[i].highlight != 0 {
					prefix += string(hl[v])
				}
			}
			suffix = string(clrReset)
		}

		if ln <= w {
			logs = append(logs, suffpref(prefix, suffix, both))
			continue
		}
		maxw := w - runewidth.StringWidth(meta) - 2

		count := 0
		lastSpace := 0
		lastCut := 0
		for ci, c := range log {
			count += runewidth.RuneWidth(c)
			if c == ' ' {
				lastSpace = ci
			}

			if count >= maxw {
				if lastSpace > lastCut {
					clean := suffpref(prefix, suffix, meta+log[lastCut:lastSpace])
					logs = append(logs, clean)
					count = ci - lastSpace
					lastCut = lastSpace + 1
					continue
				}

				clean := suffpref(prefix, suffix, meta+log[lastCut:ci-2]+"-")
				logs = append(logs, clean)
				count = 0
				lastCut = ci - 2
			}
		}

		if lastCut < runewidth.StringWidth(log) {
			clean := suffpref(prefix, suffix, meta+log[lastCut:])
			logs = append(logs, clean)
		}
	}

	if searchMatches > 0 && scrollMsg < 0 {
		ui.jumpToQueryCount = 0
	}

	if scrollMsg >= 0 {
		ui.scroll = len(logs) - scrollMsg - h/2 + 1
	}

	offset := len(logs) - nmsgs - ui.scroll
	if offset < 0 {
		ui.scroll = len(logs) - nmsgs
		offset = 0
	}
	till := offset + nmsgs
	if till >= len(logs) {
		till = len(logs)
	}
	logs = logs[offset:till]

	lat := "?ms"
	if ui.latency != nil {
		latency := int(*ui.latency / 1e6)
		lat = strconv.Itoa(latency) + "ms"
		if latency > 1000 {
			lat = ">1s"
		}
	}

	status := ui.status
	if time.Now().Before(ui.flashExpiry) {
		status = fmt.Sprintf("%s - %s", status, ui.flash)
	}
	status = pad(status, " ", w-len(lat))
	user := pad(strings.Join(ui.users, " "), " ", w)

	indent := make([]byte, ui.indent)
	for i := range indent {
		indent[i] = ' '
	}

	s := make([]byte, 0, 1024)
	s = append(s, clear...)
	s = append(s, clrStatus...)
	s = append(s, indent...)
	s = append(s, status...)
	s = append(s, lat...)
	s = append(s, clrReset...)
	s = append(s, '\r')
	s = append(s, '\n')
	s = append(s, clrUser...)
	s = append(s, indent...)
	s = append(s, user...)
	s = append(s, clrReset...)
	s = append(s, '\r')
	s = append(s, '\n')

	for _, l := range logs {
		s = append(s, indent...)
		s = append(s, l...)
		s = append(s, '\r')
		s = append(s, '\n')
	}

	for i := 0; i < h-len(logs); i++ {
		s = append(s, '\r')
		s = append(s, '\n')
	}

	if state.Song != "" {
		s = append(s, clrLine...)
		s = append(s, pad("", chrLine, rw)...)
		s = append(s, clrReset...)
		s = append(s, '\r')
		s = append(s, '\n')
		mw := rw

		duration, timeParts := formatDur(state.Duration, 2)
		position, _ := formatDur(state.Position, timeParts)
		mw -= len(duration) + len(position) + 4

		p := int(state.Pos() * float64(mw))
		if p > mw {
			p = mw
		}
		progress, rest := "", ""

		for i := p; i > 0; i-- {
			progress += chrBar
		}
		for i := mw - 1 - p; i > 0; i-- {
			rest += chrBar
		}

		rest += fmt.Sprintf(" %s/%s", position, duration)

		playStatus := chrPlaying
		if state.Paused {
			playStatus = chrPause
		}

		vol := fmt.Sprintf(" %3.f%%", state.Volume*100)
		songW := w - ui.indent - len(vol)
		song := runewidth.Truncate(state.Song, songW, "…")
		song = runewidth.FillRight(song, songW)
		s = append(s, indent...)
		s = append(s, song...)
		s = append(s, vol...)
		s = append(s, '\r')
		s = append(s, '\n')
		s = append(s, clrMusicSeek...)
		s = append(s, progress...)
		s = append(s, clrMusicIcon...)
		s = append(s, playStatus...)
		s = append(s, clrMusicSeek...)
		s = append(s, rest...)
		s = append(s, clrReset...)
		s = append(s, '\r')
		s = append(s, '\n')
	}

	s = append(s, clrLine...)
	s = append(s, pad("", chrLine, rw)...)
	s = append(s, clrReset...)
	s = append(s, '\r')
	s = append(s, '\n')
	s = append(s, indent...)
	s = append(s, ui.input...)

	if ui.cursorHide {
		s = append(s, cursorTop...)
	}
	if ui.cursorHide != ui.cursorHiddenState {
		switch ui.cursorHide {
		case false:
			s = append(s, cursorShow...)
		case true:
			s = append(s, cursorHide...)
		}
		ui.cursorHiddenState = ui.cursorHide
	}

	os.Stdout.Write(s)
}

func (ui *TermUI) Input(n byte) {
	ui.sem.Lock()
	ui.input = append(ui.input, n)
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) GetInput() string {
	ui.sem.Lock()
	s := string(ui.input)
	ui.sem.Unlock()
	return s
}

func (ui *TermUI) SetInput(n string) {
	ui.sem.Lock()
	ui.input = []byte(n)
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) BackspaceInput() {
	ui.sem.Lock()
	if len(ui.input) == 0 {
		ui.sem.Unlock()
		ui.Flush()
		return
	}
	ui.input = ui.input[:len(ui.input)-1]
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) ResetInput() []byte {
	ui.sem.Lock()
	d := make([]byte, len(ui.input))
	copy(d, ui.input)
	ui.input = ui.input[0:0]
	ui.sem.Unlock()
	ui.Flush()
	return d
}

func (ui *TermUI) CursorHide(set bool) {
	ui.cursorHide = set
	ui.Flush()
}
