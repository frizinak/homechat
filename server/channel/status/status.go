package status

import (
	"github.com/frizinak/homechat/server/channel"
)

type StatusChannel struct {
	sender  channel.Sender
	channel string

	channel.NoSave
	channel.SendOnly
	channel.NoRunClose
}

func New() *StatusChannel {
	return &StatusChannel{}
}

func (c *StatusChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *StatusChannel) Err(f channel.ClientFilter, err error) error {
	f.Channel = c.channel
	return c.sender.Broadcast(f, channel.StatusMsg{Code: channel.StatusNOK, Err: err.Error()})
}
