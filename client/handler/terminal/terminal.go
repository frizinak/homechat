package terminal

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/ui"

	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
)

type Updates interface {
	client.Logger
	Broadcast(msg []ui.Msg, scroll bool)
	JumpToActive()
	MusicState(ui.State)
	Users(string)
	Latency(time.Duration)
	Clear()
}

type Handler struct {
	log Updates

	musicState chan ui.State
	msgs       chan chatdata.ServerMessage
	songs      chan musicdata.ServerMessage

	name string
}

func New(log Updates) *Handler {
	return &Handler{
		log: log,

		musicState: make(chan ui.State, 1),
		msgs:       make(chan chatdata.ServerMessage, 8),
		songs:      make(chan musicdata.ServerMessage, 8),
	}
}

func (h *Handler) HandleName(name string) {
	h.name = name
}

func (h *Handler) HandleHistory() {
	h.log.Clear()
}

func (h *Handler) HandleLatency(l time.Duration) {
	h.log.Latency(l)
}

func (h *Handler) HandleChatMessage(m chatdata.ServerMessage) error {
	h.msgs <- m
	return nil
}

func (h *Handler) HandleMusicMessage(m musicdata.ServerMessage) error {
	h.songs <- m
	return nil
}

func (h *Handler) HandleMusicNodeMessage(musicdata.SongDataMessage) error {
	return nil
}

func (h *Handler) HandleMusicStateMessage(m client.MusicState) error {
	h.musicState <- ui.State{
		Song:     m.Title(),
		Paused:   m.Paused,
		Position: m.Position,
		Duration: m.Duration,
		Volume:   m.Volume,
	}
	return nil
}

func (h *Handler) HandleUsersMessage(m usersdata.ServerMessage, users client.Users) error {
	all := make([]string, 0, len(users))
	for _, u := range users {
		all = append(all, u.Name)
	}
	h.log.Users(strings.Join(all, " "))
	return nil
}

func (h *Handler) Run(notify chan ui.Msg) {
	go func() {
		for s := range h.musicState {
			h.log.MusicState(s)
		}
	}()

	msgsBatch := make(chan ui.Msg, 8)
	go func() {
		msgs := make([]ui.Msg, 0, 100)
		newAfter := func() <-chan time.Time {
			return time.After(time.Millisecond * 200)
		}
		after := newAfter()
		do := func() {
			if len(msgs) != 0 {
				h.log.Broadcast(msgs, true)
				msgs = msgs[:0]
			}
		}
		for {
			select {
			case msg := <-msgsBatch:
				msgs = append(msgs, msg)
			case <-time.After(time.Millisecond * 25):
				do()
			case <-after:
				after = newAfter()
				do()
			}
		}
	}()

	go func() {
		for msg := range h.msgs {
			m := ui.Msg{
				From:    msg.From,
				Stamp:   msg.Stamp,
				Message: msg.Data,
				Notify:  msg.Notify,
			}

			if msg.PM != "" {
				m.Message = fmt.Sprintf("[%s > %s] %s", msg.From, msg.PM, m.Message)
				if msg.PM == h.name {
					m.Highlight = ui.HLSlight
				}
			}

			if msg.Shout {
				m.Highlight = ui.HLActive
			}

			if msg.Bot {
				m.Highlight = ui.HLMuted
			}

			msgsBatch <- m
			if notify != nil {
				notify <- m
			}
		}
	}()

	lastMusicTitle := ""
	go func() {
		for msg := range h.songs {
			msgs := make([]ui.Msg, 0)
			msgs = append(msgs, ui.Msg{Message: msg.Title, Highlight: ui.HLTitle})
			msgs = append(msgs, ui.Msg{Message: ""})
			if msg.Text != "" {
				msgs = append(msgs, ui.Msg{Message: msg.Text, Highlight: ui.HLNone})
				msgs = append(msgs, ui.Msg{Message: ""})
			}

			n := strconv.Itoa(len(strconv.Itoa(len(msg.Songs))))
			format := "%" + n + "d) %s"
			for i, song := range msg.Songs {
				hl := ui.HLNone
				if song.Active {
					hl = ui.HLActive
				}

				if song.Problem != "" {
					hl = ui.HLProblem
				}

				msgs = append(
					msgs,
					ui.Msg{
						Message:   fmt.Sprintf(format, i+1, song.Title()),
						Highlight: hl,
					},
				)
			}

			change := lastMusicTitle != msg.Title
			lastMusicTitle = msg.Title

			h.log.Clear()
			h.log.JumpToActive()
			h.log.Broadcast(msgs, change)
		}
	}()
}
