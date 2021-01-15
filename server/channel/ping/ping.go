package ping

import (
	"io"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/ping/data"
)

type PingChannel struct {
	sender  channel.Sender
	channel string

	channel.NoSave
	channel.Limit
	channel.NoRunClose
}

func New() *PingChannel {
	return &PingChannel{Limit: channel.Limiter(255)}
}

func (c *PingChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *PingChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	_, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl)
}

func (c *PingChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	_, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl)
}

func (c *PingChannel) handle(cl channel.Client) error {
	return c.sender.Broadcast(
		channel.ClientFilter{Channel: c.channel, Client: cl},
		data.Message{},
	)
}
