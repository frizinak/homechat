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

type Visible uint8

const (
	VisibleStatus Visible = 1 << iota
	VisibleUsers
	VisibleBrowser
	VisibleSeek
	VisibleInput

	VisibleDefault = VisibleStatus | VisibleUsers | VisibleBrowser | VisibleSeek | VisibleInput
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
	metaWidth  int
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
	ui.metaWidth = 0
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
		m.Message = StripUnprintable(m.Message)
		texts := strings.Split(strings.ReplaceAll(m.Message, "\r", ""), "\n")
		for _, text := range texts {
			text = linkRE.ReplaceAllStringFunc(text, func(m string) string {
				u, err := url.Parse(m)
				if err != nil {
					return m
				}
				ui.links = append(ui.links, u)
				return fmt.Sprintf("[%d]%s", len(ui.links), m)
			})

			msg := msg{"", text, m.Highlight, nil, 0, 0}
			if ui.metaPrefix {
				msg.prefix = StripUnprintable(m.Meta)
				width := width(msg.prefix, -1)
				if width > ui.metaWidth {
					ui.metaWidth = width
				}
			}

			ptotal := 0
			for _, r := range msg.prefix {
				w := rwidth(r)
				ptotal += w
			}
			msg.pwidth = ptotal

			mwidths := make([]uint8, 0, len(msg.msg))
			mtotal := 0
			c := 0
			for _, r := range msg.msg {
				c++
				w := rwidth(r)
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

const (
	partMeta = 1
	partMsg  = 2
)

type hlSetting struct {
	v     string
	which uint8
}

var hl = map[Highlight]hlSetting{
	HLTitle:     {"\033[1m", partMeta | partMsg},
	HLActive:    {"\033[1;37;41m", partMsg},
	HLMuted:     {"\033[40;37m", partMeta | partMsg},
	HLSlight:    {"\033[1m", partMeta | partMsg},
	HLProblem:   {"\033[1;31m", partMeta | partMsg},
	HLOwn:       {"\033[32m", partMeta},
	HLTemporary: {"\033[1;32m", partMeta | partMsg},
}

type chr struct {
	v []byte
	w int
}

const (
	_chrPlaying   = "\u25B8" //"\u25BA" //"\u22D7" //"\u2023" //"‣▶"
	_chrPause     = "\u2016" //"\u2225" // ⏸"
	_chrBar       = "\u2500"
	_chrLine      = "\u2500" // "\u2E3B"
	_chrMetaSplit = "\u2502"
	_chrBottomT   = "\u2534" //"\u2538"
)

var (
	chrPlaying   = chr{[]byte(_chrPlaying), runewidth.StringWidth(_chrPlaying)}
	chrPause     = chr{[]byte(_chrPause), runewidth.StringWidth(_chrPause)}
	chrBar       = chr{[]byte(_chrBar), runewidth.StringWidth(_chrBar)}
	chrLine      = chr{[]byte(_chrLine), runewidth.StringWidth(_chrLine)}
	chrMetaSplit = chr{[]byte(_chrMetaSplit), runewidth.StringWidth(_chrMetaSplit)}
	chrBottomT   = chr{[]byte(_chrBottomT), runewidth.StringWidth(_chrBottomT)}
)

func rwidth(r rune) int { return runewidth.RuneWidth(r) }

func width(str string, runes int) int {
	c := 0
	width := 0
	for _, n := range str {
		if runes > -1 && c >= runes {
			break
		}
		width += rwidth(n)
		c++
	}

	return width
}

func padc(n string, padchr chr, total int, nWidth int) string {
	if nWidth < 0 {
		nWidth = width(n, -1)
	}
	padwidth := padchr.w
	count := total - nWidth
	rcount := count / padwidth
	padbyt := padchr.v

	if rcount <= 0 {
		return n
	}

	b := make([]byte, 0, rcount*len(padbyt))
	for i := 0; i < rcount; i++ {
		b = append(b, padbyt...)
	}
	return n + string(b)
}

func pad(n, padchr string, total int, nWidth int) string {
	return padc(n, chr{[]byte(padchr), width(padchr, -1)}, total, nWidth)
}

func suffpref(w int, prefix, suffix, meta, between, msg string, strWidth int) string {
	if prefix == "" && suffix == "" {
		return meta + between + msg
	}

	return prefix + pad(meta+between+msg, " ", w, strWidth) + suffix
}

func (ui *TermUI) logs(w int, scrollMsg *int, searchMatches *uint16) []string {
	logs := make([]string, 0, len(ui.log))
	var meta string
	var metaW int
	for i := 0; i < len(ui.log); i++ {
		if ui.metaWidth != 0 {
			metaW = ui.metaWidth + 1
			meta = pad(ui.log[i].prefix, " ", metaW, ui.log[i].pwidth)
		}
		log := ui.log[i].msg
		width := metaW + ui.log[i].mwidth

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

		extra := 0
		msgPrefix := ""
		between := string(clrReset)
		if meta != "" {
			between += string(chrMetaSplit.v)
			msgPrefix = " "
			extra = 1 + chrMetaSplit.w
			width += extra
		}
		var prefix, suffix string
		if ui.log[i].highlight > 0 {
			for v := range hl {
				if v&ui.log[i].highlight == 0 {
					continue
				}
				if hl[v].which&partMeta != 0 {
					prefix += hl[v].v
				}
				if hl[v].which&partMsg != 0 {
					between += hl[v].v
				}
			}
			suffix = string(clrReset)
		}

		maxw := w - metaW - 2

		if width <= w || maxw <= 8 {
			logs = append(
				logs,
				suffpref(w, prefix, suffix, meta, between, msgPrefix+log, width),
			)
			continue
		}

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
					width := metaW
					width += ui.log[i].mwidths[lastCutIx:lastSpaceIx].Sum() + extra
					clean := suffpref(w, prefix, suffix, meta, between, msgPrefix+log[lastCut:lastSpace], width)
					logs = append(logs, clean)
					cwidth = ui.log[i].mwidths[lastSpaceIx:ix].Sum()
					lastCut = lastSpace + 1
					lastCutIx = lastSpaceIx + 1
					continue
				}

				width := metaW
				width += ui.log[i].mwidths[lastCutIx:ix-2].Sum() + 1 + extra
				clean := suffpref(w, prefix, suffix, meta, between, msgPrefix+log[lastCut:bi2ago]+"-", width)
				logs = append(logs, clean)
				cwidth = ui.log[i].mwidths[ix-2:ix].Sum() + 1
				lastCut = bi2ago
				lastCutIx = ix - 2
			}
		}

		if lastCut < ui.log[i].mwidth {
			width := metaW
			width += ui.log[i].mwidths[lastCutIx:].Sum() + extra
			clean := suffpref(w, prefix, suffix, meta, between, msgPrefix+log[lastCut:], width)
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

	state := ui.s
	if state.Song != "" && ui.visible&VisibleSeek != 0 {
		h -= 3
	}

	if ui.visible&VisibleStatus != 0 {
		h -= 1
	}
	if ui.visible&VisibleUsers != 0 {
		h -= 1
	}
	if ui.visible&VisibleInput != 0 {
		h -= 2
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
	}
	if ui.visible&VisibleUsers != 0 {
		s = append(s, clrUser...)
		s = append(s, indent...)
		s = append(s, user...)
		s = append(s, clrReset...)
		s = append(s, '\r')
		s = append(s, '\n')
	}

	nls := 0
	for i, l := range logs {
		s = append(s, indent...)
		s = append(s, l...)
		if i != len(logs)-1 {
			nls++
			s = append(s, '\r')
			s = append(s, '\n')
		}
	}

	for i := 0; i < h-len(logs)-1; i++ {
		nls++
		s = append(s, '\r')
		s = append(s, '\n')
	}

	if nls < h-1 {
		s = append(s, '\r')
		s = append(s, '\n')
	}

	linesConnected := false
	makeLine := func() []byte {
		lineWidth := rw
		s := make([]byte, 0, lineWidth)
		if !linesConnected && ui.metaWidth != 0 {
			linesConnected = true
			p := ui.indent + ui.metaWidth + chrMetaSplit.w
			s = append(s, padc("", chrLine, p, 0)...)
			s = append(s, chrBottomT.v...)
			lineWidth -= p + chrBottomT.w
		}
		s = append(s, clrLine...)
		s = append(s, padc("", chrLine, lineWidth, 0)...)
		s = append(s, clrReset...)

		return s
	}

	if state.Song != "" && ui.visible&VisibleSeek != 0 {
		s = append(s, '\r')
		s = append(s, '\n')
		if ui.visible&VisibleBrowser != 0 {
			s = append(s, makeLine()...)
		}
		s = append(s, '\r')
		s = append(s, '\n')
		mw := w

		duration, timeParts := FormatDuration(state.Duration, 2)
		position, _ := FormatDuration(state.Position, timeParts)
		mw -= len(duration) + len(position) + 1 + 3

		p := int(state.Pos() * float64(mw))
		if p > mw {
			p = mw
		}
		progress, rest := []byte{}, []byte{}

		for i := p; i > 0; i -= chrBar.w {
			progress = append(progress, chrBar.v...)
		}
		for i := mw - 1 - p; i > 0; i -= chrBar.w {
			rest = append(rest, chrBar.v...)
		}

		playStatus := chrPlaying
		if state.Paused {
			playStatus = chrPause
		}

		vol := fmt.Sprintf(" %3.f%%", state.Volume*100)
		songW := w - len(vol) - 1
		song := runewidth.Truncate(state.Song, songW, "…")
		song = runewidth.FillRight(song, songW)
		s = append(s, indent...)
		s = append(s, song...)
		s = append(s, vol...)
		s = append(s, '\r')
		s = append(s, '\n')
		s = append(s, clrMusicIcon...)
		s = append(s, indent...)
		s = append(s, playStatus.v...)
		s = append(s, ' ')
		s = append(s, clrMusicSeekPlayed...)
		s = append(s, progress...)
		s = append(s, clrMusicSeek...)
		s = append(s, rest...)
		s = append(s, clrDuration...)
		s = append(s, fmt.Sprintf(" %s/%s", position, duration)...)
		s = append(s, clrReset...)
	}

	if ui.visible&VisibleInput != 0 {
		s = append(s, '\r')
		s = append(s, '\n')
		s = append(s, makeLine()...)
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
			rw := rwidth(r)
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
	ui.metaWidth = 0
	ui.sem.Unlock()
	ui.Flush()
	return d
}

func (ui *TermUI) CursorHide(set bool) {
	ui.cursorHide = set
	ui.Flush()
}
