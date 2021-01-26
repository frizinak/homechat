package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Amount uint16 `json:"n"`

	channel.NeverEqual
	channel.NoClose
}

func New(amount uint16) Message { return Message{Amount: amount} }

func (m Message) Binary(w channel.BinaryWriter) error {
	w.WriteUint16(m.Amount)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error)   { return JSONMessage(r) }

func BinaryMessage(r channel.BinaryReader) (Message, error) {
	c := Message{}
	c.Amount = r.ReadUint16()
	return c, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	c := Message{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerMessage struct {
	channel.NilMsg
}

func (m ServerMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}

func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryServerMessage(r channel.BinaryReader) (ServerMessage, error) {
	n, err := channel.BinaryNilMessage(r)
	c := ServerMessage{n}
	return c, err
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	n, nr, err := channel.JSONNilMessage(r)
	c := ServerMessage{n}
	return c, nr, err
}
