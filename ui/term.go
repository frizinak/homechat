package ui

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/containerd/console"
	"github.com/mattn/go-runewidth"
)

const stampFormat = "01-02 15:04"

type Visible uint8

const (
	VisibleStatus Visible = 1 << iota
	VisibleBrowser
	VisibleSeek
	VisibleInput

	VisibleDefault = VisibleStatus | VisibleBrowser | VisibleSeek | VisibleInput
)

var linkRE = regexp.MustCompile(`https?://[^\s]+`)

type user struct {
	name   string
	typing bool
}

type cache struct {
	invalid bool
	w, h    int
	log     []string
}

func (c *cache) Invalidate() { c.invalid = true }
func (c *cache) Update(w, h int) bool {
	b := c.invalid || c.w != w || c.h != h
	c.w, c.h, c.invalid = w, h, false
	return b
}

// assumes utf-8
type TermUI struct {
	metaPrefix bool
	indent     int
	scrollTop  bool

	status      string
	flash       string
	flashExpiry time.Time

	latency *time.Duration

	sem sync.Mutex

	log []msg

	cache *cache
	input []byte
	users []*user

	links []*url.URL

	cursorcol         int
	scrollPage        int
	scrollSimple      int
	scroll            int
	jumpToActive      bool
	jumpToQuery       string
	jumpToQueryUpdate bool
	jumpToQueryCount  uint16

	maxMessages int
	visible     Visible

	cursorHide        bool
	cursorHiddenState bool

	s State

	disabled bool
}

type widths []uint8

func (w widths) Sum() int {
	t := 0
	for _, v := range w {
		t += int(v)
	}
	return t
}

type msg struct {
	prefix    string
	msg       string
	highlight Highlight
	pwidths   widths
	mwidths   widths
	pwidth    int
	mwidth    int
}

func Term(metaPrefix bool, maxMessages, indent int, scrollTop bool, visible Visible) *TermUI {
	return &TermUI{
		metaPrefix:  metaPrefix,
		indent:      indent,
		scrollTop:   scrollTop,
		visible:     visible,
		maxMessages: maxMessages,
		disabled:    true,
		links:       make([]*url.URL, 0),
		cursorHide:  visible&VisibleInput == 0,
		cache:       &cache{invalid: true},
	}
}

func (ui *TermUI) Start() {
	ui.disabled = false
	ui.Flush()
}

func (ui *TermUI) Users(users []string) {
	ui.sem.Lock()

	for i := range users {
		users[i] = StripUnprintable(users[i])
	}

	nm := make(map[string]*user, len(users))
	for _, u := range ui.users {
		nm[u.name] = u
	}

	u := make([]*user, 0, len(users))
	for _, n := range users {
		if ex, ok := nm[n]; ok {
			u = append(u, ex)
			continue
		}

		u = append(u, &user{n, false})
	}

	ui.users = u
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) UserTyping(who string, is bool) {
	ui.sem.Lock()
	for _, u := range ui.users {
		if u.name == StripUnprintable(who) {
			u.typing = is
		}
	}
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) Link(id string) (*url.URL, bool) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	n--
	ui.sem.Lock()
	defer ui.sem.Unlock()
	if n >= len(ui.links) {
		return nil, false
	}

	return ui.links[n], true
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
	ui.flash = StripUnprintable(msg)
	ui.Flush()
}

func (ui *TermUI) Log(msg string)    { ui.status = StripUnprintable(msg); ui.Flush() }
func (ui *TermUI) ErrStr(err string) { ui.status = StripUnprintable(err); ui.Flush() }
func (ui *TermUI) Err(err error)     { ui.ErrStr(err.Error()) }

func (ui *TermUI) Clear() {
	ui.sem.Lock()
	ui.log = make([]msg, 0)
	ui.cache.Invalidate()
	ui.sem.Unlock()
}

func (ui *TermUI) JumpToActive() { ui.jumpToActive = true }
func (ui *TermUI) Search(qry string) {
	qry = strings.ToLower(qry)
	ui.sem.Lock()
	ui.jumpToQueryUpdate = true
	same := ui.jumpToQuery == qry
	ui.jumpToQuery = qry
	ui.jumpToQueryCount++
	if !same {
		ui.jumpToQueryCount = 1
	}

	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) Broadcast(msgs []Msg, scroll bool) {
	if ui.visible&VisibleBrowser == 0 {
		return
	}
	ui.sem.Lock()
	for _, m := range msgs {
		m.From = StripUnprintable(m.From)
		m.Message = StripUnprintable(m.Message)
		texts := strings.Split(strings.ReplaceAll(m.Message, "\r", ""), "\n")
		for _, text := range texts {
			text = linkRE.ReplaceAllStringFunc(text, func(m string) string {
				u, err := url.Parse(m)
				if err != nil {
					return m
				}
				ui.links = append(ui.links, u)
				return fmt.Sprintf("\033[1m[%d]\033[0m%s", len(ui.links), m)
			})

			msg := msg{"", text, m.Highlight, nil, nil, 0, 0}
			if ui.metaPrefix {
				msg.prefix = fmt.Sprintf(
					"%s %s",
					m.Stamp.Format(stampFormat),
					m.From,
				)
				msg.prefix = runewidth.FillRight(msg.prefix, len(stampFormat)+15+1) + "│ "
			}

			pwidths := make([]uint8, 0, len(msg.prefix))
			ptotal := 0
			for _, r := range msg.prefix {
				w := runewidth.RuneWidth(r)
				pwidths = append(pwidths, uint8(w))
				ptotal += w
			}
			msg.pwidths = pwidths
			msg.pwidth = ptotal

			mwidths := make([]uint8, 0, len(msg.msg))
			mtotal := 0
			c := 0
			for _, r := range msg.msg {
				c++
				w := runewidth.RuneWidth(r)
				mwidths = append(mwidths, uint8(w))
				mtotal += w
			}
			msg.mwidths = mwidths
			msg.mwidth = mtotal

			ui.log = append(ui.log, msg)
		}
	}

	if len(ui.log) > ui.maxMessages {
		ui.log = ui.log[len(ui.log)-ui.maxMessages:]
	}
	if ui.scrollTop && scroll {
		ui.scroll = math.MaxInt32
	}
	ui.cache.Invalidate()
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) MusicState(s State) {
	if s == ui.s {
		return
	}

	s.Song = StripUnprintable(s.Song)

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

func (ui *TermUI) setcursor(val int) {
	ui.cursorcol = val
	if ui.cursorcol < 0 {
		ui.cursorcol = 0
	}

	l := utf8.RuneCount(ui.input)
	if ui.cursorcol > l {
		ui.cursorcol = l
	}
}

func (ui *TermUI) Left() {
	ui.sem.Lock()
	ui.setcursor(ui.cursorcol - 1)
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) Right() {
	ui.sem.Lock()
	ui.setcursor(ui.cursorcol + 1)
	ui.sem.Unlock()
	ui.Flush()
}

var (
	cursorTop          = []byte("\033[H")
	cursorHide         = []byte("\033[?25l")
	cursorShow         = []byte("\033[?25h")
	clear              = []byte("\033[H\033[J")
	clrLine            = []byte("\033[1m")
	clrStatus          = []byte("\033[40;37m")
	clrUser            = []byte("\033[40;37m")
	clrReset           = []byte("\033[0m")
	clrMusicSeek       = []byte("\033[1;30m")
	clrMusicSeekPlayed = []byte("\033[1;32m")
	clrMusicIcon       = []byte("\033[0;32m")
	clrDuration        = []byte("\033[0;32m")
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
	chrBar     = "\u2500" //\u23AF" //"\u22EF"
	chrLine    = "\u2E3B"
)

func width(str string, runes int) int {
	c := 0
	width := 0
	for _, n := range str {
		if runes > -1 && c >= runes {
			break
		}
		width += runewidth.RuneWidth(n)
		c++
	}

	return width
}

func pad(n, padchr string, total int, nWidth int) string {
	if nWidth < 0 {
		nWidth = width(n, -1)
	}
	padwidth := width(padchr, -1)
	count := total - nWidth
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

func suffpref(w int, prefix, suffix, str string, strWidth int) string {
	if prefix == "" && suffix == "" {
		return str
	}
	str = pad(str, " ", w, strWidth)
	str = prefix + str
	str = str + suffix
	return str
}

func (ui *TermUI) logs(w int, scrollMsg *int, searchMatches *uint16) []string {
	logs := make([]string, 0, len(ui.log))
	for i := 0; i < len(ui.log); i++ {
		meta := ui.log[i].prefix
		log := ui.log[i].msg
		both := meta + log

		width := ui.log[i].pwidth + ui.log[i].mwidth

		if ui.jumpToActive && ui.log[i].highlight&HLActive != 0 {
			ui.jumpToActive = false
			*scrollMsg = len(logs) + 1
		}

		if ui.jumpToQueryUpdate {
			ui.log[i].highlight &= ^HLTemporary
			if ui.jumpToQuery != "" && strings.Contains(strings.ToLower(log), ui.jumpToQuery) {
				*searchMatches++
				if *searchMatches == ui.jumpToQueryCount {
					*scrollMsg = len(logs) + 1
					ui.log[i].highlight |= HLTemporary
				}
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

		if width <= w {
			logs = append(logs, suffpref(w, prefix, suffix, both, width))
			continue
		}
		maxw := w - ui.log[i].pwidth - 2

		cwidth := 0
		lastSpace, lastSpaceIx := 0, 0
		lastCut, lastCutIx := 0, 0
		ix := -1
		byteIndex1Ago, byteIndex2Ago := 0, 0
		for byteIndex, c := range log {
			ix++
			cwidth += int(ui.log[i].mwidths[ix])
			if c == ' ' {
				lastSpace = byteIndex
				lastSpaceIx = ix
			}

			bi2ago := byteIndex2Ago
			byteIndex2Ago = byteIndex1Ago
			byteIndex1Ago = byteIndex

			if cwidth >= maxw {
				if lastSpace > lastCut {
					width := ui.log[i].pwidth
					width += ui.log[i].mwidths[lastCutIx:lastSpaceIx].Sum()
					clean := suffpref(w, prefix, suffix, meta+log[lastCut:lastSpace], width)
					logs = append(logs, clean)
					cwidth = ui.log[i].mwidths[lastSpaceIx:ix].Sum()
					lastCut = lastSpace + 1
					lastCutIx = lastSpaceIx + 1
					continue
				}

				width := ui.log[i].pwidth
				width += ui.log[i].mwidths[lastCutIx:ix-2].Sum() + 1
				clean := suffpref(w, prefix, suffix, meta+log[lastCut:bi2ago]+"-", -1)
				logs = append(logs, clean)
				cwidth = ui.log[i].mwidths[ix-2:ix].Sum() + 1
				lastCut = bi2ago
				lastCutIx = ix - 2
			}
		}

		if lastCut < ui.log[i].mwidth {
			width := ui.log[i].pwidth
			width += ui.log[i].mwidths[lastCutIx:].Sum()
			clean := suffpref(w, prefix, suffix, meta+log[lastCut:], width)
			logs = append(logs, clean)
		}
	}

	return logs
}

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

	h -= 4
	state := ui.s
	if state.Song != "" && ui.visible&VisibleSeek != 0 {
		h -= 3
	}

	if ui.visible&VisibleStatus == 0 {
		h += 2
	}
	if ui.visible&VisibleInput == 0 {
		h += 3
	}

	ui.scroll += ui.scrollPage * h / 2
	ui.scroll += ui.scrollSimple
	ui.scrollPage = 0
	ui.scrollSimple = 0
	if ui.scroll < 0 {
		ui.scroll = 0
	}

	if w < 10 {
		w = 10
	}
	rw := w
	w -= ui.indent

	nmsgs := h
	if nmsgs < 0 {
		nmsgs = 0
	}

	logs := ui.cache.log
	scrollMsg := -1
	var searchMatches uint16

	if logs == nil || ui.jumpToActive || ui.jumpToQueryUpdate || ui.cache.Update(w, h) {
		logs = ui.logs(w, &scrollMsg, &searchMatches)
		ui.cache.log = logs
	}

	ui.jumpToQueryUpdate = false
	if searchMatches > 0 && scrollMsg < 0 {
		ui.jumpToQueryCount = 0
	}

	if scrollMsg >= 0 {
		ui.scroll = len(logs) - scrollMsg - h/2 + 1
		if ui.scroll < 0 {
			ui.scroll = 0
		}
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

	status = runewidth.Truncate(status, w-len(lat), "…")
	status = pad(status, " ", w-len(lat), -1)
	users := make([]string, 0, len(ui.users))
	for _, u := range ui.users {
		typ := " "
		if u.typing {
			typ = "…"
		}
		users = append(users, fmt.Sprintf("%s%s", u.name, typ))
	}
	user := pad(strings.Join(users, " "), " ", w, -1)

	indent := make([]byte, ui.indent)
	for i := range indent {
		indent[i] = ' '
	}

	s := make([]byte, 0, 1024)
	s = append(s, clear...)
	if ui.visible&VisibleStatus != 0 {
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
	}

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

	if state.Song != "" && ui.visible&VisibleSeek != 0 {
		if ui.visible&VisibleBrowser != 0 {
			s = append(s, clrLine...)
			s = append(s, pad("", chrLine, rw, -1)...)
			s = append(s, clrReset...)
		}
		s = append(s, '\r')
		s = append(s, '\n')
		mw := rw

		duration, timeParts := FormatDuration(state.Duration, 2)
		position, _ := FormatDuration(state.Position, timeParts)
		mw -= len(duration) + len(position) + 4 + 3

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
		s = append(s, clrMusicIcon...)
		s = append(s, indent...)
		s = append(s, playStatus...)
		s = append(s, ' ')
		s = append(s, clrMusicSeekPlayed...)
		s = append(s, progress...)
		s = append(s, clrMusicSeek...)
		s = append(s, rest...)
		s = append(s, clrDuration...)
		s = append(s, fmt.Sprintf(" %s/%s", position, duration)...)
		s = append(s, clrReset...)
		if ui.visible&VisibleInput != 0 {
			s = append(s, '\r')
			s = append(s, '\n')
		}
	}

	if ui.visible&VisibleInput != 0 {
		s = append(s, clrLine...)
		s = append(s, pad("", chrLine, rw, -1)...)
		s = append(s, clrReset...)
		s = append(s, '\r')
		s = append(s, '\n')
		s = append(s, indent...)

		input := string(ui.input)
		inputCW := width(input, ui.cursorcol)
		off := 0
		max := w - 2
		if inputCW > max {
			off = inputCW - max
			inputCW = max
		}

		cw := 0
		sliceMin, sliceMax := len(ui.input), len(ui.input)
		for i, r := range input {
			rw := runewidth.RuneWidth(r)
			cw += rw
			if cw >= off+rw && i < sliceMin {
				inputCW += off + rw - cw
				sliceMin = i
			}

			if cw > max+off {
				sliceMax = i
				break
			}
		}

		s = append(s, ui.input[sliceMin:sliceMax]...)
		s = append(s, fmt.Sprintf("\033[0G\033[%dC", inputCW+ui.indent)...)
	}

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

	ns := make([]byte, 0, len(ui.input)+1)
	pre := true
	c := 0
	cursorcol := 0
	for len(ui.input) > 0 {
		if pre {
			cursorcol++
		}

		if pre && c == ui.cursorcol {
			ns = append(ns, n)
			pre = false
		}

		_, size := utf8.DecodeRune(ui.input)
		ns = append(ns, ui.input[:size]...)
		ui.input = ui.input[size:]

		c++
	}

	if pre {
		ns = append(ns, n)
		cursorcol++
	}

	ui.input = ns
	ui.setcursor(cursorcol)

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
	ui.setcursor(utf8.RuneCount(ui.input))
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

	ns := make([]byte, 0, len(ui.input))
	c := 0
	for len(ui.input) > 0 {
		_, size := utf8.DecodeRune(ui.input)
		if c != ui.cursorcol-1 {
			ns = append(ns, ui.input[:size]...)
		}
		ui.input = ui.input[size:]
		c++
	}
	ui.input = ns

	ui.setcursor(ui.cursorcol - 1)
	ui.sem.Unlock()
	ui.Flush()
}

func (ui *TermUI) ResetInput() []byte {
	ui.sem.Lock()
	d := make([]byte, len(ui.input))
	copy(d, ui.input)
	ui.input = ui.input[0:0]
	ui.setcursor(0)
	ui.sem.Unlock()
	ui.Flush()
	return d
}

func (ui *TermUI) CursorHide(set bool) {
	ui.cursorHide = set
	ui.Flush()
}
