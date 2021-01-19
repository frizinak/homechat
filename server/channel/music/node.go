package music

import (
	"io"
	"log"
	"os"
	"sync"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
)

type MusicNodeChannel struct {
	sem sync.Mutex

	col *collection.Collection

	log *log.Logger

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.Limit
	channel.NoRunClose
}

func NewMusicNode(log *log.Logger, col *collection.Collection) *MusicNodeChannel {
	return &MusicNodeChannel{log: log, col: col, Limit: channel.Limiter(255)}
}

func (c *MusicNodeChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *MusicNodeChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryNodeMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *MusicNodeChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONNodeMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *MusicNodeChannel) handle(cl channel.Client, m data.NodeMessage) error {
	filter := channel.ClientFilter{Client: cl, Channel: c.channel}
	path := c.col.SongPath(m.Song())
	fh, err := os.Open(path)
	if os.IsNotExist(err) {
		return c.sender.Broadcast(filter, data.NewNoSongDataMessage())
	}

	if err != nil {
		return err
	}

	stat, err := fh.Stat()
	if err != nil {
		fh.Close()
		return err
	}

	// sending is async, .Binary will take care of closing
	return c.sender.Broadcast(
		filter,
		data.NewSongDataMessage(
			m.NS,
			m.ID,
			stat.Size(),
			fh,
		),
	)
}
