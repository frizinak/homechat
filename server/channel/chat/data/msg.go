package data

import (
	"encoding/json"
	"io"
	"time"

	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Data string `json:"d"`

	channel.NeverEqual
}

func (m Message) Binary(w channel.BinaryWriter) error {
	w.WriteString(m.Data, 32)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r channel.BinaryReader) (channel.Msg, error) { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error)   { return JSONMessage(r) }

func BinaryMessageFromReader(r io.Reader) Message {
	return Message{}
}

func BinaryMessage(r channel.BinaryReader) (Message, error) {
	c := Message{}
	c.Data = r.ReadString(32)
	return c, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	c := Message{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type Notify byte

const (
	NotifyDefault  Notify = 0
	NotifyPersonal Notify = 1 << iota
	NotifyNever
)

type ServerMessage struct {
	Message

	From   string    `json:"from"`
	Stamp  time.Time `json:"stamp"`
	PM     string    `json:"pm"`
	Notify Notify    `json:"notify"`
	Bot    bool      `json:"bot"`
	Shout  bool      `json:"shout"`

	channel.NeverEqual
}

func (m ServerMessage) Binary(w channel.BinaryWriter) error {
	var bot, shout byte
	if m.Bot {
		bot = 1
	}
	if m.Shout {
		shout = 1
	}

	if err := m.Message.Binary(w); err != nil {
		return err
	}

	w.WriteString(m.From, 8)
	w.WriteUint64(uint64(m.Stamp.Unix()))
	w.WriteString(m.PM, 8)
	w.WriteUint8(byte(m.Notify))
	w.WriteUint8(bot)
	w.WriteUint8(shout)
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
	msg.Message, err = BinaryMessage(r)
	if err != nil {
		return
	}

	msg.From = r.ReadString(8)
	msg.Stamp = time.Unix(int64(r.ReadUint64()), 0)
	msg.PM = r.ReadString(8)
	msg.Notify = Notify(r.ReadUint8())
	msg.Bot = r.ReadUint8() == 1
	msg.Shout = r.ReadUint8() == 1
	return msg, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	c := ServerMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
