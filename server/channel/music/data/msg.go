package data

import (
	"encoding/json"
	"io"

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
