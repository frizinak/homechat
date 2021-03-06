package music

import (
	"log"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/player"
)

type StateChannel struct {
	log *log.Logger
	p   *player.Player

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.SendOnly
	channel.NoRunClose
}

func NewState(l *log.Logger, p *player.Player) *StateChannel {
	return &StateChannel{log: l, p: p}
}

func (c *StateChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *StateChannel) Send() {
	f := channel.ClientFilter{Channel: c.channel}
	state := data.ServerStateMessage{}
	state.Paused = c.p.Paused()
	state.Duration = c.p.Duration()
	state.Position = c.p.Position()
	state.Volume = c.p.Volume()
	if err := c.sender.Broadcast(f, state); err != nil {
		c.log.Println(err)
	}
}
