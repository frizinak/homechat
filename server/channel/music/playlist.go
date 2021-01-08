package music

import (
	"log"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
)

type PlaylistChannel struct {
	log *log.Logger
	col *collection.Collection

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.SendOnlyChannel
}

func NewPlaylist(l *log.Logger, col *collection.Collection) *PlaylistChannel {
	return &PlaylistChannel{log: l, col: col}
}

func (c *PlaylistChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *PlaylistChannel) Send() {
	f := channel.ClientFilter{Channel: c.channel}
	ps := data.ServerPlaylistMessage{c.col.List()}
	if err := c.sender.Broadcast(f, ps); err != nil {
		c.log.Println(err)
	}
}
