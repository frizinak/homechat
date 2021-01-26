package data

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type NodeMessage struct {
	NS       string
	ID       string
	Playlist string

	channel.NoClose
}

func (m NodeMessage) Binary(r channel.BinaryWriter) error {
	r.WriteString(m.NS, 8)
	r.WriteString(m.ID, 8)
	r.WriteString(m.Playlist, 16)
	return r.Err()
}

func (m NodeMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m NodeMessage) Equal(msg channel.Msg) bool { return m == msg }

func (m NodeMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryNodeMessage(r)
}

func (m NodeMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONNodeMessage(r)
}

func BinaryNodeMessage(r channel.BinaryReader) (NodeMessage, error) {
	m := NodeMessage{}
	m.NS = r.ReadString(8)
	m.ID = r.ReadString(8)
	m.Playlist = r.ReadString(16)
	return m, r.Err()
}

func JSONNodeMessage(r io.Reader) (NodeMessage, io.Reader, error) {
	m := NodeMessage{}
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}

type SongDataMessage struct {
	Available bool
	Song      Song
	Size      int64

	r io.Reader

	channel.NeverEqual
}

func NewSongDataMessage(song Song, size int64, r io.Reader) SongDataMessage {
	return SongDataMessage{Available: true, Song: song, Size: size, r: r}
}

func NewNoSongDataMessage() SongDataMessage {
	return SongDataMessage{Available: false}
}

func (m SongDataMessage) Upload() io.Reader {
	return m.r
}

func (m SongDataMessage) Binary(w channel.BinaryWriter) error {
	if !m.Available {
		w.WriteUint8(0)
		return w.Err()
	}
	w.WriteUint8(1)
	if err := m.Song.Binary(w); err != nil {
		return err
	}
	w.WriteUint64(uint64(m.Size))
	if err := w.Err(); err != nil {
		return err
	}

	conn := w.Writer()
	_, err := io.Copy(conn, m.r)

	return err
}

func (m SongDataMessage) Close() error {
	if rc, ok := m.r.(io.ReadCloser); ok {
		return rc.Close()
	}

	return nil
}

func (m SongDataMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinarySongDataMessage(r)
}

func (m SongDataMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONSongDataMessage(r)
}

func (m SongDataMessage) JSON(w io.Writer) error {
	return errors.New("can't serialize song data")
}

func BinarySongDataMessage(r channel.BinaryReader) (SongDataMessage, error) {
	m := SongDataMessage{}
	m.Available = r.ReadUint8() == 1
	if !m.Available {
		return m, r.Err()
	}

	s, err := BinarySong(r)
	if err != nil {
		return m, err
	}
	m.Song = s
	m.Size = int64(r.ReadUint64())
	m.r = io.LimitReader(r.Reader(), m.Size)
	return m, r.Err()
}

func JSONSongDataMessage(r io.Reader) (SongDataMessage, io.Reader, error) {
	return SongDataMessage{}, r, errors.New("can't deserialize song data")
}
