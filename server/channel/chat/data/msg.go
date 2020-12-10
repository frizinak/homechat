package data

import (
	"encoding/json"
	"io"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	Data string `json:"d"`

	*channel.NeverEqual
}

func (m Message) Binary(w *binary.Writer) error {
	w.WriteString(m.Data, 32)
	return w.Err()
}

func (m Message) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m Message) FromBinary(r *binary.Reader) (channel.Msg, error)     { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) { return JSONMessage(r) }

func BinaryMessageFromReader(r io.Reader) Message {
	return Message{}
}

func BinaryMessage(r *binary.Reader) (Message, error) {
	c := Message{}
	c.Data = r.ReadString(32)
	return c, r.Err()
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	c := Message{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}

type ServerMessage struct {
	Message

	From   string    `json:"from"`
	Stamp  time.Time `json:"stamp"`
	PM     string    `json:"pm"`
	Notify bool      `json:"notify"`
	Bot    bool      `json:"bot"`

	*channel.NeverEqual
}

func (m ServerMessage) Binary(w *binary.Writer) error {
	var notify, bot byte
	if m.Notify {
		notify = 1
	}
	if m.Bot {
		bot = 1
	}

	if err := m.Message.Binary(w); err != nil {
		return err
	}

	w.WriteString(m.From, 8)
	w.WriteUint64(uint64(m.Stamp.Unix()))
	w.WriteString(m.PM, 8)
	w.WriteUint8(notify)
	w.WriteUint8(bot)
	return w.Err()
}

func (m ServerMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerMessage) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return BinaryServerMessage(r)
}
func (m ServerMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerMessage(r)
}

func BinaryServerMessage(r *binary.Reader) (msg ServerMessage, err error) {
	msg.Message, err = BinaryMessage(r)
	if err != nil {
		return
	}

	msg.From = r.ReadString(8)
	msg.Stamp = time.Unix(int64(r.ReadUint64()), 0)
	msg.PM = r.ReadString(8)
	msg.Notify = r.ReadUint8() == 1
	msg.Bot = r.ReadUint8() == 1
	return msg, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	c := ServerMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
