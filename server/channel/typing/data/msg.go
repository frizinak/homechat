package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Channel string `json:"channel"`

	channel.NoClose
	channel.NeverEqual
}

func (m Message) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Channel, 8)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryMessage(r)
}

func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryMessage(r channel.BinaryReader) (msg Message, err error) {
	msg.Channel = r.ReadString(8)
	return msg, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	c := Message{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerMessage struct {
	Channel string `json:"channel"`
	Who     string `json:"who"`

	channel.NoClose
	channel.NeverEqual
}

func (m ServerMessage) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Channel, 8)
	w.WriteString(m.Who, 8)
	return w.Err()
}

func (m ServerMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerMessage) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}

func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryServerMessage(r channel.BinaryReader) (msg ServerMessage, err error) {
	msg.Channel = r.ReadString(8)
	msg.Who = r.ReadString(8)
	return msg, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	c := ServerMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
