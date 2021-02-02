package data

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	GOOS   string
	GOARCH string

	channel.NoClose
}

func (m Message) Binary(r channel.BinaryWriter) error {
	r.WriteString(m.GOOS, 8)
	r.WriteString(m.GOARCH, 8)
	return r.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) Equal(msg channel.Msg) bool { return m == msg }

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryMessage(r)
}

func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONMessage(r)
}

func BinaryMessage(r channel.BinaryReader) (Message, error) {
	m := Message{}
	m.GOOS = r.ReadString(8)
	m.GOARCH = r.ReadString(8)
	return m, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	m := Message{}
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}

type ServerMessage struct {
	Available bool
	Sig       []byte
	Size      int64

	r io.Reader

	channel.NeverEqual
}

func NewServerMessage(size int64, sig []byte, r io.Reader) ServerMessage {
	return ServerMessage{Available: true, Sig: sig, Size: size, r: r}
}

func NewNoServerMessage() ServerMessage {
	return ServerMessage{Available: false}
}

func (m ServerMessage) Upload() io.Reader {
	return m.r
}

func (m ServerMessage) Binary(w channel.BinaryWriter) error {
	if !m.Available {
		w.WriteUint8(0)
		return w.Err()
	}
	w.WriteUint8(1)
	w.WriteBytes(m.Sig, 32)
	w.WriteUint64(uint64(m.Size))
	if err := w.Err(); err != nil {
		return err
	}

	conn := w.Writer()
	_, err := io.Copy(conn, m.r)

	return err
}

func (m ServerMessage) Close() error {
	if rc, ok := m.r.(io.ReadCloser); ok {
		return rc.Close()
	}

	return nil
}

func (m ServerMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}

func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func (m ServerMessage) JSON(w io.Writer) error {
	return errors.New("can't serialize update data")
}

func BinaryServerMessage(r channel.BinaryReader) (ServerMessage, error) {
	m := ServerMessage{}
	m.Available = r.ReadUint8() == 1
	if !m.Available {
		return m, r.Err()
	}

	m.Sig = r.ReadBytes(32)
	m.Size = int64(r.ReadUint64())
	m.r = io.LimitReader(r.Reader(), m.Size)
	return m, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	return ServerMessage{}, r, errors.New("can't deserialize update data")
}
