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

func (c *MusicNodeChannel) sendSong(f channel.ClientFilter, s collection.Song) error {
	path, err := s.File()
	if err != nil {
		return err
	}

	fh, err := os.Open(path)
	if os.IsNotExist(err) {
		return c.sender.Broadcast(f, data.NewNoSongDataMessage())
	}

	if err != nil {
		return err
	}

	stat, err := fh.Stat()
	if err != nil {
		fh.Close()
		return err
	}

	song := data.Song{P_NS: s.NS(), P_ID: s.ID(), P_Title: s.Title()}

	// sending is async, .Binary will take care of closing
	return c.sender.Broadcast(
		f,
		data.NewSongDataMessage(song, stat.Size(), fh),
	)
}

func (c *MusicNodeChannel) handle(cl channel.Client, m data.NodeMessage) error {
	filter := channel.ClientFilter{Client: cl, Channel: c.channel}
	if m.NS == "" && m.ID == "" {
		songs, err := c.col.PlaylistSongs(m.Playlist)
		if err == collection.ErrNotExists {
			return nil
		}
		if err != nil {
			return err
		}

		for _, s := range songs {
			if err := c.sendSong(filter, s); err != nil {
				return err
			}
		}
	}

	song, err := c.col.Find(m.NS, m.ID)
	if err == collection.ErrSongNotExists {
		return c.sender.Broadcast(filter, data.NewNoSongDataMessage())
	}
	if err != nil {
		return err
	}

	return c.sendSong(filter, song)
}
