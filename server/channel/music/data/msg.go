package data

import (
	"encoding/json"
	"io"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Command string `json:"cmd"`

	*channel.NeverEqual
}

func (m Message) Binary(w *binary.Writer) error {
	w.WriteString(m.Command, 16)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r *binary.Reader) (channel.Msg, error)     { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) { return JSONMessage(r) }

func BinaryMessage(r *binary.Reader) (Message, error) {
	c := Message{}
	c.Command = r.ReadString(16)
	return c, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	c := Message{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type Song struct {
	Title  string `json:"title"`
	Active bool   `json:"a"`
}

type ServerMessage struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Songs []Song `json:"songs"`
}

func (m ServerMessage) Binary(w *binary.Writer) error {
	var a uint8
	w.WriteString(m.Title, 16)
	w.WriteString(m.Text, 32)
	w.WriteUint32(uint32(len(m.Songs)))
	for _, s := range m.Songs {
		a = 0
		if s.Active {
			a = 1
		}
		w.WriteString(s.Title, 8)
		w.WriteUint8(a)
	}
	return w.Err()
}

func (m ServerMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerMessage) Equal(msg channel.Msg) bool {
	rm, ok := msg.(ServerMessage)
	if !ok ||
		m.Title != rm.Title ||
		m.Text != rm.Text ||
		len(m.Songs) != len(rm.Songs) {

		return false
	}

	for i := range m.Songs {
		if m.Songs[i] != rm.Songs[i] {
			return false
		}
	}
	return true
}

func (m ServerMessage) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}
func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryServerMessage(r *binary.Reader) (ServerMessage, error) {
	m := ServerMessage{}
	m.Title = r.ReadString(16)
	m.Text = r.ReadString(32)
	n := r.ReadUint32()
	m.Songs = make([]Song, n)
	for i := range m.Songs {
		m.Songs[i] = Song{r.ReadString(8), r.ReadUint8() == 1}
	}
	return m, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	m := ServerMessage{}
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}

type ServerSongMessage struct {
	Song string `json:"song"`
}

func (m ServerSongMessage) Equal(msg channel.Msg) bool { return m == msg }

func (m ServerSongMessage) Binary(w *binary.Writer) error {
	w.WriteString(m.Song, 8)
	return w.Err()
}

func (m ServerSongMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerSongMessage) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return BinaryServerSongMessage(r)
}
func (m ServerSongMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerSongMessage(r)
}

func BinaryServerSongMessage(r *binary.Reader) (ServerSongMessage, error) {
	c := ServerSongMessage{}
	c.Song = r.ReadString(8)
	return c, r.Err()
}

func JSONServerSongMessage(r io.Reader) (ServerSongMessage, io.Reader, error) {
	c := ServerSongMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerStateMessage struct {
	Paused   bool          `json:"paused"`
	Position time.Duration `json:"position"`
	Duration time.Duration `json:"duration"`
	Volume   float64       `json:"volume"`
}

func (m ServerStateMessage) Equal(msg channel.Msg) bool {
	m1, ok := msg.(ServerStateMessage)
	if !ok {
		return false
	}
	return m1.Paused == m.Paused &&
		m1.Position == m.Position &&
		m1.Duration == m.Duration &&
		m1.Volume == m.Volume
}

func (m ServerStateMessage) Binary(w *binary.Writer) error {
	var pause uint8
	if m.Paused {
		pause = 1
	}
	vol := uint8(255 * m.Volume)

	w.WriteUint8(pause)
	w.WriteUint32(uint32(m.Position / time.Second))
	w.WriteUint32(uint32(m.Duration / time.Second))
	w.WriteUint8(vol)
	return w.Err()
}

func (m ServerStateMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerStateMessage) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return BinaryServerStateMessage(r)
}
func (m ServerStateMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerStateMessage(r)
}

func BinaryServerStateMessage(r *binary.Reader) (ServerStateMessage, error) {
	c := ServerStateMessage{}
	c.Paused = r.ReadUint8() == 1
	c.Position = time.Second * time.Duration(r.ReadUint32())
	c.Duration = time.Second * time.Duration(r.ReadUint32())
	c.Volume = float64(r.ReadUint8()) / 255
	return c, r.Err()
}

func JSONServerStateMessage(r io.Reader) (ServerStateMessage, io.Reader, error) {
	c := ServerStateMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
