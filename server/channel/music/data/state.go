package data

import (
	"encoding/json"
	"io"
	"time"

	"github.com/frizinak/homechat/server/channel"
)

type ServerStateMessage struct {
	Paused   bool          `json:"paused"`
	Position time.Duration `json:"position"`
	Duration time.Duration `json:"duration"`
	Volume   float64       `json:"volume"`
	Delay    time.Duration `json:"delay"`

	start time.Time
}

func (m ServerStateMessage) Prepare() ServerStateMessage {
	m.start = time.Now()
	return m
}

func (m ServerStateMessage) Equal(msg channel.Msg) bool {
	m1, ok := msg.(ServerStateMessage)
	if !ok {
		return false
	}
	return m1.Paused == m.Paused &&
		m1.Position/time.Second == m.Position/time.Second &&
		m1.Duration/time.Second == m.Duration/time.Second &&
		m1.Volume == m.Volume
}

func (m ServerStateMessage) Binary(w channel.BinaryWriter) error {
	var pause uint8
	if m.Paused {
		pause = 1
	}
	vol := uint8(255 * m.Volume)

	w.WriteUint8(pause)
	w.WriteUint32(uint32(m.Position / time.Millisecond))
	w.WriteUint32(uint32(m.Duration / time.Millisecond))
	w.WriteUint32(uint32(time.Since(m.start) / time.Millisecond))
	w.WriteUint8(vol)
	return w.Err()
}

func (m ServerStateMessage) JSON(w io.Writer) error {
	m.Delay = time.Since(m.start)
	return json.NewEncoder(w).Encode(m)
}

func (m ServerStateMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerStateMessage(r)
}

func (m ServerStateMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerStateMessage(r)
}

func BinaryServerStateMessage(r channel.BinaryReader) (ServerStateMessage, error) {
	c := ServerStateMessage{}
	c.Paused = r.ReadUint8() == 1
	c.Position = time.Millisecond * time.Duration(r.ReadUint32())
	c.Duration = time.Millisecond * time.Duration(r.ReadUint32())
	c.Delay = time.Millisecond * time.Duration(r.ReadUint32())
	c.Volume = float64(r.ReadUint8()) / 255
	return c, r.Err()
}

func JSONServerStateMessage(r io.Reader) (ServerStateMessage, io.Reader, error) {
	c := ServerStateMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerSongMessage struct {
	NS    string `json:"ns"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (m ServerSongMessage) Equal(msg channel.Msg) bool { return m == msg }

func (m ServerSongMessage) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.NS, 8)
	w.WriteString(m.ID, 8)
	w.WriteString(m.Title, 8)
	return w.Err()
}

func (m ServerSongMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerSongMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerSongMessage(r)
}

func (m ServerSongMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerSongMessage(r)
}

func BinaryServerSongMessage(r channel.BinaryReader) (ServerSongMessage, error) {
	c := ServerSongMessage{}
	c.NS = r.ReadString(8)
	c.ID = r.ReadString(8)
	c.Title = r.ReadString(8)
	return c, r.Err()
}

func JSONServerSongMessage(r io.Reader) (ServerSongMessage, io.Reader, error) {
	c := ServerSongMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
