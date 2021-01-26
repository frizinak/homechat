package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Command string `json:"cmd"`

	channel.NeverEqual
	channel.NoClose
}

func (m Message) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Command, 16)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error)   { return JSONMessage(r) }

func BinaryMessage(r channel.BinaryReader) (Message, error) {
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
	P_NS    string `json:"ns"`
	P_ID    string `json:"id"`
	P_Title string `json:"title"`
	Active  bool   `json:"a"`
	Problem string `json:"problem"`
}

func (s Song) NS() string    { return s.P_NS }
func (s Song) ID() string    { return s.P_ID }
func (s Song) Title() string { return s.P_Title }

func (s Song) Binary(w channel.BinaryWriter) error {
	var a uint8 = 0
	if s.Active {
		a = 1
	}
	w.WriteString(s.P_NS, 8)
	w.WriteString(s.P_ID, 8)
	w.WriteString(s.P_Title, 8)
	w.WriteUint8(a)
	w.WriteString(s.Problem, 8)
	return w.Err()
}

func BinarySong(r channel.BinaryReader) (Song, error) {
	return Song{
		r.ReadString(8),
		r.ReadString(8),
		r.ReadString(8),
		r.ReadUint8() == 1,
		r.ReadString(8),
	}, r.Err()
}

type ServerMessage struct {
	View  byte   `json:"view"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Songs []Song `json:"songs"`

	channel.NoClose
}

func (m ServerMessage) Binary(w channel.BinaryWriter) error {
	w.WriteUint8(m.View)
	w.WriteString(m.Title, 16)
	w.WriteString(m.Text, 32)
	w.WriteUint32(uint32(len(m.Songs)))
	for _, s := range m.Songs {
		if err := s.Binary(w); err != nil {
			return err
		}
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

func (m ServerMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}

func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryServerMessage(r channel.BinaryReader) (ServerMessage, error) {
	m := ServerMessage{}
	m.View = r.ReadUint8()
	m.Title = r.ReadString(16)
	m.Text = r.ReadString(32)
	n := r.ReadUint32()
	m.Songs = make([]Song, n)
	for i := range m.Songs {
		s, err := BinarySong(r)
		if err != nil {
			return m, err
		}
		m.Songs[i] = s
	}
	return m, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	m := ServerMessage{}
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}
