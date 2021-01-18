package musicnode

import (
	"fmt"
	"io"
	"time"

	"github.com/frizinak/homechat/client"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/youtube"
)

type Handler struct {
	col *collection.Collection
	q   *collection.Queue
	p   *player.Player

	lastS collection.Song
}

func New(col *collection.Collection, q *collection.Queue, p *player.Player) *Handler {
	return &Handler{col: col, q: q, p: p}
}

func (h *Handler) song(state client.MusicState) (collection.Song, bool, error) {
	var s collection.Song
	c := h.q.Current()
	if c != nil && c.Song != nil {
		s = c.Song
	}
	if s != nil && s.NS() == state.NS && s.ID() == state.ID {
		return s, true, nil
	}

	switch state.NS {
	case collection.NSYoutube:
		r := youtube.NewResult(state.ID, state.Title)
		s = h.col.FromYoutube(r)
		return s, false, nil
	default:
		return s, false, fmt.Errorf("unsupported song ns %s", state.NS)
	}
}

func (h *Handler) HandleMusicStateMessage(state client.MusicState) error {
	s, inQueue, err := h.song(state)
	if err != nil {
		return err
	}

	if !inQueue {
		h.q.Reset()
		h.col.QueueSong(s)
	}

	pos := h.p.Position()
	if state.Position/time.Second != pos/time.Second {
		if pos > state.Position {
			h.p.Seek(state.Position, io.SeekStart)
		} else if state.Position-pos > time.Second {
			h.p.Seek(state.Position, io.SeekStart)
			fmt.Println("out of sync, seeking", state.Position.Seconds(), h.p.Position().Seconds())
		}
	}

	if state.Paused {
		h.p.Pause()
		return nil
	} else if h.p.Paused() {
		h.p.Play()
	}

	if s == h.lastS {
		return nil
	}

	h.p.ForcePlay()
	h.lastS = s
	return nil
}

func (h *Handler) HandleName(name string)        {}
func (h *Handler) HandleHistory()                {}
func (h *Handler) HandleLatency(d time.Duration) {}

func (h *Handler) HandleChatMessage(chatdata.ServerMessage) error {
	return nil
}

func (h *Handler) HandleMusicMessage(musicdata.ServerMessage) error {
	return nil
}

func (h *Handler) HandleUsersMessage(usersdata.ServerMessage, client.Users) error {
	return nil
}
