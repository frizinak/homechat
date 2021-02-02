package update

import (
	"bytes"
	"io"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/update/data"
)

type CB func(GOOS, GOARCH string) (sig []byte, data []byte, ok bool)

type UpdateChannel struct {
	channel string
	sender  channel.Sender

	cb CB

	channel.NoSave
	channel.Limit
	channel.NoRunClose
}

func New(cb CB) *UpdateChannel {
	return &UpdateChannel{cb: cb, Limit: channel.Limiter(255)}
}

func (c *UpdateChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *UpdateChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *UpdateChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *UpdateChannel) send(f channel.ClientFilter, m data.Message) error {
	sig, buf, ok := c.cb(m.GOOS, m.GOARCH)
	if !ok {
		return c.sender.Broadcast(f, data.NewNoServerMessage())
	}

	return c.sender.Broadcast(
		f,
		data.NewServerMessage(int64(len(buf)), sig, bytes.NewReader(buf)),
	)
}

func (c *UpdateChannel) handle(cl channel.Client, m data.Message) error {
	filter := channel.ClientFilter{Client: cl, Channel: c.channel}
	return c.send(filter, m)
}
