package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type PlaylistSongsMessage struct {
	Playlist string `json:"playlist"`

	channel.NeverEqual
	channel.NoClose
}

func (m PlaylistSongsMessage) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Playlist, 16)
	return w.Err()
}

func (m PlaylistSongsMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m PlaylistSongsMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryPlaylistSongsMessage(r)
}

func (m PlaylistSongsMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONPlaylistSongsMessage(r)
}

func BinaryPlaylistSongsMessage(r channel.BinaryReader) (PlaylistSongsMessage, error) {
	c := PlaylistSongsMessage{}
	c.Playlist = r.ReadString(16)
	return c, r.Err()
}

func JSONPlaylistSongsMessage(r io.Reader) (PlaylistSongsMessage, io.Reader, error) {
	c := PlaylistSongsMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerPlaylistSongsMessage struct {
	List []Song `json:"list"`

	channel.NeverEqual
	channel.NoClose
}

func (m ServerPlaylistSongsMessage) Binary(w channel.BinaryWriter) error {
	w.WriteUint32(uint32(len(m.List)))
	for _, p := range m.List {
		if err := p.Binary(w); err != nil {
			return err
		}
	}
	return w.Err()
}

func (m ServerPlaylistSongsMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerPlaylistSongsMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerPlaylistSongsMessage(r)
}

func (m ServerPlaylistSongsMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerPlaylistSongsMessage(r)
}

func BinaryServerPlaylistSongsMessage(r channel.BinaryReader) (ServerPlaylistSongsMessage, error) {
	c := ServerPlaylistSongsMessage{}
	n := r.ReadUint32()
	c.List = make([]Song, n)
	var i uint32
	for ; i < n; i++ {
		s, err := BinarySong(r)
		if err != nil {
			return c, err
		}

		c.List[i] = s
	}
	return c, r.Err()
}

func JSONServerPlaylistSongsMessage(r io.Reader) (ServerPlaylistSongsMessage, io.Reader, error) {
	c := ServerPlaylistSongsMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
