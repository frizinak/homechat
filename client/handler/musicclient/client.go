package musicclient

import (
	"fmt"
	"strings"
	"sync"

	"github.com/frizinak/homechat/client"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"
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
	sem     sync.Mutex
	logger  client.Logger
	handler client.Handler

	title string
	songs []ui.Song
	text  string

	mode mode
}

func newOutput(handler client.Handler, logger client.Logger) *output {
	return &output{logger: logger, handler: handler, mode: modeNone}
}

func (s *output) SetTitle(title string) { s.title = title }
func (s *output) SetSongs(l []ui.Song) {
	s.songs = l
	s.mode = modeSongs
}

func (s *output) SetText(str string) {
	s.text = str
	s.mode = modeText
}

func (s *output) AtomicFlush(cb func()) {
	s.sem.Lock()
	cb()
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
			msg.Songs[i] = musicdata.Song{song.NS(), song.ID(), song.Title(), song.Active()}
		}

	case modeText:
		msg.Text = s.text
	}

	s.handler.HandleMusicMessage(msg)
}

func (s *output) Err(e error) {
	s.logger.Err(e.Error())
}

func (s *output) Errf(f string, v ...interface{}) {
	s.logger.Err(fmt.Sprintf(f, v...))
}

type commandParser struct {
	ui.Parser
	cl            *client.Client
	localCommands map[ui.CommandType]struct{}
}

func newCommandParser(parser ui.Parser, client *client.Client) *commandParser {
	return &commandParser{
		cl:     client,
		Parser: parser,
		localCommands: map[ui.CommandType]struct{}{
			ui.CmdPlay:         {},
			ui.CmdPause:        {},
			ui.CmdPauseToggle:  {},
			ui.CmdNext:         {},
			ui.CmdPrev:         {},
			ui.CmdSetSongIndex: {},
			ui.CmdSongDelete:   {},
			ui.CmdSeek:         {},
			ui.CmdQueue:        {},
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

	if len(remote) != 0 {
		// todo error
		if err := c.cl.Music(strings.Join(remote, ";")); err != nil {
			panic(err)
		}
	}

	return filtered
}

type UI struct {
	*base.UI
	handler client.Handler
}

func NewUI(handler client.Handler, logger client.Logger, di *di.DI, cl *client.Client) *UI {
	col := di.Collection()
	col.Run()
	output := newOutput(handler, logger)
	rhandler := newHandler(handler, col)
	parser := newCommandParser(di.CommandParser(), cl)
	ui := base.New(
		output,
		output,
		parser,
		di.Player(),
		col,
		di.Queue(),
	)

	rhandler.ui = ui
	return &UI{UI: ui, handler: rhandler}
}

func (ui *UI) Handler() client.Handler { return ui.handler }

type handler struct {
	client.Handler
	ui  *base.UI
	col *collection.Collection
}

func newHandler(h client.Handler, col *collection.Collection) *handler {
	return &handler{Handler: h, col: col}
}

func (h *handler) HandleMusicMessage(m musicdata.ServerMessage) error {
	if m.Text != "" {
		return h.Handler.HandleMusicMessage(m)
	}

	l := make([]collection.Song, 0, len(m.Songs))
	for _, s := range m.Songs {
		switch s.NS {
		case collection.NSYoutube:
			rs := h.col.FromYoutube(youtube.NewResult(s.ID, s.Title))
			l = append(l, rs)
		default:
			return fmt.Errorf("song with unknown namespace: '%s'", s.NS)
		}
	}

	h.ui.SetExternal(m.Title, l)
	return h.Handler.HandleMusicMessage(m)
}
