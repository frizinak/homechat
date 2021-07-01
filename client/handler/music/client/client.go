package client

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/handler/music"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/ui"
	"github.com/frizinak/libym/ui/base"
	"github.com/frizinak/libym/youtube"
)

type mode int

const (
	modeNone mode = iota
	modeSongs
	modeText
)

type output struct {
	*music.ErrorFlasher
	sem     sync.Mutex
	handler client.Handler

	view  ui.View
	title string
	songs []ui.Song
	text  string

	problematics *collection.Problematics

	mode mode
}

func newOutput(handler client.Handler, logger client.Logger, problems *collection.Problematics) *output {
	return &output{
		ErrorFlasher: music.NewErrorFlasher(logger),
		handler:      handler,
		mode:         modeNone,
		problematics: problems,
	}
}

func (s *output) SetView(view ui.View)  { s.view = view }
func (s *output) SetTitle(title string) { s.title = title }
func (s *output) SetSongs(l []ui.Song) {
	s.songs = l
	s.mode = modeSongs
}

func (s *output) SetText(str string) {
	s.text = str
	s.mode = modeText
}

func (s *output) AtomicFlush(cb func(ui.AtomicOutput)) {
	s.sem.Lock()
	cb(s)
	s.flush()
	s.sem.Unlock()
}

func (s *output) Flush() {
	s.sem.Lock()
	s.flush()
	s.sem.Unlock()
}

func (s *output) flush() {
	if s.mode == modeNone {
		return
	}

	msg := musicdata.ServerMessage{Title: s.title}
	switch s.mode {
	case modeSongs:
		msg.Songs = make([]musicdata.Song, len(s.songs))
		for i, song := range s.songs {
			t := song.Title()
			e := song.Extra()
			if e != "" {
				t += e
			}
			msg.Songs[i] = musicdata.Song{
				song.NS(),
				song.ID(),
				t,
				song.Active(),
				s.problematics.Reason(song),
			}
		}

	case modeText:
		msg.Text = s.text
	}

	s.handler.HandleMusicMessage(msg)
}

type commandParser struct {
	ui.Parser
	cl            *client.Client
	localCommands map[ui.CommandType]struct{}
	offline       bool
	handler       *handler
	logger        client.Logger
}

func newCommandParser(offline bool, handler *handler, logger client.Logger, parser ui.Parser, client *client.Client) *commandParser {
	return &commandParser{
		cl:      client,
		Parser:  parser,
		offline: offline,
		handler: handler,
		logger:  logger,
		localCommands: map[ui.CommandType]struct{}{
			ui.CmdPlay:         {},
			ui.CmdPause:        {},
			ui.CmdPauseToggle:  {},
			ui.CmdNext:         {},
			ui.CmdPrev:         {},
			ui.CmdSetSongIndex: {},
			ui.CmdSeek:         {},
			ui.CmdQueue:        {},
			ui.CmdQueueAfter:   {},
			ui.CmdQueueShuffle: {},
			ui.CmdQueueClear:   {},
			ui.CmdViewQueue:    {},
			ui.CmdVolume:       {},
		},
	}
}

func (c *commandParser) Parse(s string) []ui.Command {
	commands := c.Parser.Parse(s)
	filtered := make([]ui.Command, 0, len(commands))
	remote := make([]string, 0)
	for _, command := range commands {
		if _, local := c.localCommands[command.Type()]; !local {
			str := fmt.Sprintf("%s %s", command.Cmd(), command.Args().String())
			remote = append(remote, str)
			continue
		}

		filtered = append(filtered, command)
	}

	if c.offline {
		return filtered
	}

	if len(remote) != 0 {
		last := c.handler.lastMsg
		if last != nil {
			c.handler.HandleMusicMessage(*last)
		}
		if err := c.cl.Music(strings.Join(remote, ";")); err != nil {
			c.logger.Err(err)
		}
	}

	return filtered
}

type UI struct {
	*base.UI
	handler client.Handler
	p       *player.Player
	q       *collection.Queue
	closed  bool

	lsong  musicdata.ServerSongMessage
	lstate musicdata.ServerStateMessage

	connSem sync.RWMutex
	conns   []net.Conn
	server  net.Listener
}

func NewUI(offline bool, handler client.Handler, logger client.Logger, di *di.DI, cl *client.Client) *UI {
	col := di.Collection()
	rate1, rate2 := di.Rates()
	col.Run(rate1, rate2)
	output := newOutput(handler, logger, col.Problematics())
	rhandler := newHandler(handler, col)
	parser := newCommandParser(offline, rhandler, logger, di.CommandParser(), cl)
	p := di.Player()
	q := di.Queue()
	ui := base.New(output, output, parser, p, col, q, nil)

	p.LoadPosition()

	rhandler.ui = ui
	return &UI{UI: ui, handler: rhandler, p: p, q: q}
}

func (ui *UI) ListenAndServe(addr string) error {
	server, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	ui.server = server

	for {
		conn, err := server.Accept()
		if err != nil {
			continue
		}

		ms := client.MusicState{ui.lstate, ui.lsong}
		if _, err := fmt.Fprintln(conn, ms.Format("\t")); err != nil {
			continue
		}
		ui.connSem.Lock()
		ui.conns = append(ui.conns, conn)
		ui.connSem.Unlock()
	}

}

func (ui *UI) Flush() {
	if ui.closed {
		return
	}

	state := musicdata.ServerStateMessage{}
	song := musicdata.ServerSongMessage{}
	cur := ui.q.Current()
	s := cur.Song
	if s != nil {
		song.Song = musicdata.Song{P_NS: s.NS(), P_ID: s.ID(), P_Title: s.Title()}
	}
	state.Paused = ui.p.Paused()
	state.Duration = ui.p.Duration()
	state.Position = ui.p.Position()
	state.Volume = ui.p.Volume()

	if ui.lstate.Equal(state) && ui.lsong.Equal(song) {
		return
	}
	ui.lstate, ui.lsong = state, song

	ms := client.MusicState{state, song}
	ui.handler.HandleMusicStateMessage(ms)

	ui.connSem.RLock()
	for i := 0; i < len(ui.conns); i++ {
		c := ui.conns[i]
		if _, err := fmt.Fprintln(c, ms.Format("\t")); err != nil {
			ui.conns = append(ui.conns[:i], ui.conns[i+1:]...)
			i--
		}
	}
	ui.connSem.RUnlock()
}

func (ui *UI) Close() error {
	ui.closed = true
	_ = ui.p.SavePosition()
	if ui.server != nil {
		_ = ui.server.Close()
	}

	return ui.p.Close()
}

func (ui *UI) Handler() client.Handler { return ui.handler }

type handler struct {
	client.Handler
	ui      *base.UI
	col     *collection.Collection
	lastMsg *musicdata.ServerMessage
}

func newHandler(h client.Handler, col *collection.Collection) *handler {
	return &handler{Handler: h, col: col}
}

func (h *handler) HandleMusicMessage(m musicdata.ServerMessage) error {
	switch ui.View(m.View) {
	case ui.ViewQueue:
		h.lastMsg = nil
		return nil
	}

	if m.Text != "" {
		m.Title = fmt.Sprintf("external: %s", m.Title)
		h.lastMsg = &m
		return h.Handler.HandleMusicMessage(m)
	}

	l := make([]collection.Song, 0, len(m.Songs))
	for _, s := range m.Songs {
		switch s.NS() {
		case collection.NSYoutube:
			rs := h.col.FromYoutube(youtube.NewResult(s.ID(), s.Title()))
			l = append(l, rs)
		default:
			return fmt.Errorf("song with unknown namespace: '%s'", s.NS())
		}
	}

	h.lastMsg = &m
	h.ui.SetExternal(m.Title, l)
	h.ui.Refresh()
	return nil
}
