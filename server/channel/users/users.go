package users

import (
	"io"
	"time"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/users/data"
)

type UsersChannel struct {
	usersChannels []string
	col           channel.UserCollection

	sender  channel.Sender
	channel string

	channel.NoSave

	change bool
	err    error

	channel.Limit
	channel.NoRunClose
}

func New(channels []string, col channel.UserCollection) *UsersChannel {
	return &UsersChannel{usersChannels: channels, col: col, Limit: channel.Limiter(255)}
}

func (c *UsersChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *UsersChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *UsersChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *UsersChannel) UserUpdate(channel.Client, channel.ConnectionReason) error {
	c.change = true
	return c.err
}

func (c *UsersChannel) handle(cl channel.Client, m data.Message) error {
	return c.do(channel.ClientFilter{Client: cl, Channel: c.channel})
}

func (c *UsersChannel) SendInterval(iv time.Duration) {
	for {
		time.Sleep(iv)
		if !c.change {
			continue
		}
		c.change = false
		if err := c.do(channel.ClientFilter{Channel: c.channel}); err != nil {
			c.err = err
		}
	}
}

func (c *UsersChannel) do(f channel.ClientFilter) error {
	for _, ch := range c.usersChannels {
		f.HasChannel = []string{ch}
		users := c.col.GetUsers(ch)
		s := data.ServerMessage{Channel: ch, Users: make([]data.User, len(users))}
		for i, u := range users {
			s.Users[i] = data.User{u.Name, uint8(u.Clients)}
		}

		if err := c.sender.Broadcast(f, s); err != nil {
			return err
		}
	}
	return nil
}
