package status

import (
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type StatusChannel struct {
	sender  channel.Sender
	channel string

	channel.NoSave
}

func New() *StatusChannel {
	return &StatusChannel{}
}

func (c *StatusChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *StatusChannel) HandleBIN(cl channel.Client, r *binary.Reader) error {
	return errors.New("does not support receiving messages")
}

func (c *StatusChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	return r, errors.New("does not support receiving messages")
}

func (c *StatusChannel) Err(f channel.ClientFilter, err error) error {
	f.Channel = c.channel
	return c.sender.Broadcast(f, channel.StatusMsg{Code: channel.StatusNOK, Err: err.Error()})
}
