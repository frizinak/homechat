package data

import (
	"errors"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Filename string
	Message  string
	Size     int64

	r io.Reader

	channel.NeverEqual
	channel.NoClose
}

func NewMessage(filename, msg string, size int64, r io.Reader) Message {
	return Message{Filename: filename, Message: msg, Size: size, r: r}
}

func (m Message) Upload() io.Reader {
	return m.r
}

func (m Message) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Filename, 8)
	w.WriteString(m.Message, 16)
	w.WriteUint64(uint64(m.Size))
	if err := w.Err(); err != nil {
		return err
	}

	conn := w.Writer()
	_, err := io.Copy(conn, m.r)
	return err
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error)   { return JSONMessage(r) }

func (m Message) JSON(w io.Writer) error {
	return errors.New("can't serialize an upload")
}

func BinaryMessage(r channel.BinaryReader) (Message, error) {
	m := Message{}
	m.Filename = r.ReadString(8)
	m.Message = r.ReadString(16)
	m.Size = int64(r.ReadUint64())
	m.r = io.LimitReader(r.Reader(), m.Size)
	return m, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	return Message{}, r, errors.New("can't deserialize an upload")
}
