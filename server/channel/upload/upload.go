package upload

import (
	"errors"
	"fmt"
	"io"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/chat"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	"github.com/frizinak/homechat/server/channel/upload/data"
)

type UploadChannel struct {
	broadcast *chat.ChatChannel
	uploader  channel.Uploader

	sender  channel.Sender
	channel string

	channel.NoSave
	channel.Limit
	channel.NoRunClose
}

func New(max int64, broadcastChannel *chat.ChatChannel, uploader channel.Uploader) *UploadChannel {
	return &UploadChannel{
		broadcast: broadcastChannel,
		uploader:  uploader,
		Limit:     channel.Limiter(max),
	}
}

func (c *UploadChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *UploadChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}

	u, err := c.uploader.Upload(m.Filename, m.Upload())
	if err != nil {
		return err
	}

	msg := u.String()
	if m.Message != "" {
		msg = fmt.Sprintf("%s %s", m.Message, msg)
	}

	return c.broadcast.Handle(cl, chatdata.Message{Data: msg})
}

func (c *UploadChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	return r, errors.New("not implemented by design")
}
