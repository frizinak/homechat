package handler

import (
	"time"

	"github.com/frizinak/homechat/client"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
)

type NoopHandler struct {
}

func (h NoopHandler) HandleName(string)                                              {}
func (h NoopHandler) HandleHistory()                                                 {}
func (h NoopHandler) HandleLatency(time.Duration)                                    {}
func (h NoopHandler) HandleChatMessage(chatdata.ServerMessage) error                 { return nil }
func (h NoopHandler) HandleMusicMessage(musicdata.ServerMessage) error               { return nil }
func (h NoopHandler) HandleMusicNodeMessage(musicdata.SongDataMessage) error         { return nil }
func (h NoopHandler) HandleMusicStateMessage(client.MusicState) error                { return nil }
func (h NoopHandler) HandleUsersMessage(usersdata.ServerMessage, client.Users) error { return nil }
