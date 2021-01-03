package ping

import (
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/ping/data"
)

type PingChannel struct {
	sender  channel.Sender
	channel string

	channel.NoSave
	channel.Limit
}

func New() *PingChannel {
	return &PingChannel{Limit: channel.Limiter(255)}
}

func (c *PingChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *PingChannel) HandleBIN(cl channel.Client, r *binary.Reader) error {
	_, err := data.BinaryMessage(r)
	return err
}

func (c *PingChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	_, nr, err := data.JSONMessage(r)
	return nr, err
}
