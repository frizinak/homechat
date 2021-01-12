package data

import (
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Message struct {
	channel.NilMsg
}

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
