package musicclient

import (
	"fmt"
	"strings"
	"sync"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/server/channel/music/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/di"
	"github.com/frizinak/libym/ui"
	"github.com/frizinak/libym/ui/base"
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
			title := strings.TrimSpace(song.Title())
			if title == "" {
				title = fmt.Sprintf("- no title - [%s %s]", song.NS(), song.ID())
			}
			msg.Songs[i] = data.Song{title, song.Active()}
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

type UI struct {
	*base.UI
}

func NewUI(handler client.Handler, logger client.Logger, di *di.DI) *UI {
	output := newOutput(handler, logger)
	base := base.New(
		output,
		output,
		di.CommandParser(),
		di.Player(),
		di.Collection(),
		di.Queue(),
	)

	return &UI{UI: base}
}
