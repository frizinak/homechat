package data

import (
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Filename string
	Message  string
	r        io.Reader
	*channel.NeverEqual
}

func NewMessage(filename, msg string, r io.Reader) Message {
	return Message{filename, msg, r, nil}
}

func (m Message) Reader() io.Reader {
	return m.r
}

func (m Message) Binary(w *binary.Writer) error {
	w.WriteString(m.Filename, 8)
	w.WriteString(m.Message, 16)
	if err := w.Err(); err != nil {
		return err
	}

	_, err := io.Copy(w.Writer(), m.r)
	return err
}

func (m Message) FromBinary(r *binary.Reader) (channel.Msg, error)     { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) { return JSONMessage(r) }

func (m Message) JSON(w io.Writer) error {
	return errors.New("can't serialize an upload")
}

func BinaryMessage(r *binary.Reader) (Message, error) {
	m := Message{}
	m.Filename = r.ReadString(8)
	m.Message = r.ReadString(16)
	m.r = r.Reader()
	return m, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	return Message{}, r, errors.New("can't deserialize an upload")
}
