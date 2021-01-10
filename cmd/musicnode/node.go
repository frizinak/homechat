package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/frizinak/homechat/client"
	"github.com/frizinak/homechat/client/tcp"
	"github.com/frizinak/homechat/server/channel"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/homechat/ui"
	"github.com/frizinak/homechat/vars"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/di"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/youtube"
)

type Handler struct {
	col *collection.Collection
	q   *collection.Queue
	p   *player.Player

	lastS collection.Song
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

func (h *Handler) HandleName(name string) {}
func (h *Handler) HandleHistory()         {}

func (h *Handler) HandleChatMessage(chatdata.ServerMessage) error {
	return nil
}

func (h *Handler) HandleMusicMessage(musicdata.ServerMessage) error {
	return nil
}

func (h *Handler) HandleUsersMessage(usersdata.ServerMessage, client.Users) error {
	return nil
}

func main() {
	log := log.New(os.Stdout, "", 0)
	// todo
	server := "127.0.0.1:1200"
	tcpConf := tcp.Config{Domain: strings.TrimSpace(server)}
	clientConf := client.Config{}
	clientConf.Name = strings.TrimSpace("music-node")
	clientConf.Framed = false
	clientConf.Proto = channel.ProtoBinary
	clientConf.ServerURL = "http://" + tcpConf.Domain
	clientConf.History = 0
	clientConf.Channels = []string{
		vars.MusicChannel,
		vars.MusicStateChannel,
		vars.MusicSongChannel,
		vars.MusicPlaylistChannel,
	}

	// todo
	storePath := "./tmp-music-node"
	musicConfig := di.Config{
		Log:          log,
		StorePath:    storePath,
		MPVLogger:    ioutil.Discard,
		AutoSave:     true,
		SimpleOutput: ioutil.Discard,
	}
	di := di.New(musicConfig)

	handler := &Handler{col: di.Collection(), q: di.Queue(), p: di.Player()}
	tcp, err := tcp.New(tcpConf)
	if err != nil {
		panic(err)
	}

	client := client.New(tcp, handler, ui.Plain(os.Stdout), clientConf)
	panic(client.Run())
}
