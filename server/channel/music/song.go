package music

import (
	"log"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
)

type SongChannel struct {
	log *log.Logger
	q   *collection.Queue

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.SendOnly
	channel.NoRunClose
}

func NewSong(l *log.Logger, q *collection.Queue) *SongChannel {
	return &SongChannel{log: l, q: q}
}

func (c *SongChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *SongChannel) Send() {
	f := channel.ClientFilter{Channel: c.channel}
	song := data.ServerSongMessage{}
	cur := c.q.Current()
	s := cur.Song
	if s != nil {
		song.Song = data.Song{P_NS: s.NS(), P_ID: s.ID(), P_Title: s.Title()}
	}
	if err := c.sender.Broadcast(f, song); err != nil {
		c.log.Println(err)
	}
}
