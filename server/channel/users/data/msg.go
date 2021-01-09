package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	*channel.NilMsg
}

func (m Message) FromBinary(r *binary.Reader) (channel.Msg, error)     { return BinaryMessage(r) }
func (m Message) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) { return JSONMessage(r) }

func BinaryMessage(r *binary.Reader) (Message, error) {
	n, err := channel.BinaryNilMessage(r)
	c := Message{n}
	return c, err
}

func JSONMessage(r io.Reader) (Message, io.Reader, error) {
	n, nr, err := channel.JSONNilMessage(r)
	c := Message{n}
	return c, nr, err
}

type User struct {
	Name    string `json:"name"`
	Clients uint8  `json:"clients"`
}

type ServerMessage struct {
	Channel string `json:"channel"`
	Users   []User `json:"users"`
}

func (m ServerMessage) Binary(w *binary.Writer) error {
	w.WriteString(m.Channel, 8)
	w.WriteUint16(uint16(len(m.Users)))
	for _, u := range m.Users {
		w.WriteString(u.Name, 8)
		w.WriteUint8(u.Clients)
	}
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

func (m ServerMessage) Equal(msg channel.Msg) bool {
	rm, ok := msg.(ServerMessage)
	if !ok ||
		m.Channel != rm.Channel ||
		len(m.Users) != len(rm.Users) {
		return false
	}

	for i := range m.Users {
		if m.Users[i] != rm.Users[i] {
			return false
		}
	}
	return true
}

func BinaryServerMessage(r *binary.Reader) (msg ServerMessage, err error) {
	msg.Channel = r.ReadString(8)
	msg.Users = make([]User, r.ReadUint16())
	for i := range msg.Users {
		msg.Users[i].Name = r.ReadString(8)
		msg.Users[i].Clients = r.ReadUint8()
	}
	return msg, r.Err()
}

func JSONServerMessage(r io.Reader) (ServerMessage, io.Reader, error) {
	c := ServerMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}
