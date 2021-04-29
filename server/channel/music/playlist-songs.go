package music

import (
	"io"
	"log"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
)

type PlaylistSongsChannel struct {
	log *log.Logger
	col *collection.Collection

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.Limit
	channel.NoRunClose
}

func NewPlaylistSongs(l *log.Logger, col *collection.Collection) *PlaylistSongsChannel {
	return &PlaylistSongsChannel{log: l, col: col, Limit: channel.Limiter(255)}
}

func (c *PlaylistSongsChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *PlaylistSongsChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryPlaylistSongsMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *PlaylistSongsChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONPlaylistSongsMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *PlaylistSongsChannel) handle(cl channel.Client, m data.PlaylistSongsMessage) error {
	f := channel.ClientFilter{Client: cl, Channel: c.channel}
	songs, err := c.col.PlaylistSongs(m.Playlist)
	if err != nil {
		// ignore
	}

	s := make([]data.Song, len(songs))
	for i, song := range songs {
		s[i] = data.Song{
			P_NS:    song.NS(),
			P_ID:    song.ID(),
			P_Title: song.Title(),
		}
	}

	ps := data.ServerPlaylistSongsMessage{List: s}
	if err := c.sender.Broadcast(f, ps); err != nil {
		c.log.Println(err)
	}
	return nil
}
