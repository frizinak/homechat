package typing

import (
	"io"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/typing/data"
)

type TypingChannel struct {
	typingChannels map[string]struct{}

	sender  channel.Sender
	channel string

	channel.NoRunClose
	channel.NoSave
	channel.Limit
}

func New(channels []string) *TypingChannel {
	cmap := make(map[string]struct{})
	for i := range channels {
		cmap[channels[i]] = struct{}{}
	}
	return &TypingChannel{typingChannels: cmap, Limit: channel.Limiter(255)}
}

func (c *TypingChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *TypingChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *TypingChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *TypingChannel) handle(cl channel.Client, m data.Message) error {
	if _, ok := c.typingChannels[m.Channel]; !ok {
		return nil
	}

	f := channel.ClientFilter{Channel: c.channel}
	f.HasChannel = []string{m.Channel}
	s := data.ServerMessage{Channel: m.Channel, Who: cl.Name()}
	return c.sender.Broadcast(f, s)
}
