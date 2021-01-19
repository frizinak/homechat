package data

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/libym/collection"
)

type SongID struct {
	ns  string
	id  string
	gid string
}

func (s *SongID) NS() string { return s.ns }
func (s *SongID) ID() string { return s.id }
func (s *SongID) GlobalID() string {
	if s.gid == "" {
		s.gid = collection.GlobalID(s)
	}

	return s.gid
}

type NodeMessage struct {
	NS string
	ID string
}

func (m NodeMessage) Song() *SongID { return &SongID{ns: m.NS, id: m.ID} }

func (m NodeMessage) Binary(r channel.BinaryWriter) error {
	r.WriteString(m.NS, 8)
	r.WriteString(m.ID, 8)
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
	return m, r.Err()
}

func JSONNodeMessage(r io.Reader) (NodeMessage, io.Reader, error) {
	m := NodeMessage{}
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}

type SongDataMessage struct {
	Available bool
	NS        string
	ID        string
	Size      int64

	r io.Reader

	channel.NeverEqual
}

func NewSongDataMessage(ns, id string, size int64, r io.Reader) SongDataMessage {
	return SongDataMessage{Available: true, NS: ns, ID: id, Size: size, r: r}
}

func NewNoSongDataMessage() SongDataMessage {
	return SongDataMessage{Available: false}
}

func (m SongDataMessage) Song() *SongID {
	return &SongID{ns: m.NS, id: m.ID}
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
	w.WriteString(m.NS, 8)
	w.WriteString(m.ID, 8)
	w.WriteUint64(uint64(m.Size))
	if err := w.Err(); err != nil {
		return err
	}

	conn := w.Writer()
	_, err := io.Copy(conn, m.r)
	if rc, ok := m.r.(io.ReadCloser); ok {
		rc.Close()
	}

	return err
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

	m.NS = r.ReadString(8)
	m.ID = r.ReadString(8)
	m.Size = int64(r.ReadUint64())
	m.r = io.LimitReader(r.Reader(), m.Size)
	return m, r.Err()
}

func JSONSongDataMessage(r io.Reader) (SongDataMessage, io.Reader, error) {
	return SongDataMessage{}, r, errors.New("can't deserialize song data")
}
