package music

import (
	"log"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
)

type StateChannel struct {
	log *log.Logger
	p   *player.Player
	q   *collection.Queue

	channel string
	sender  channel.Sender

	channel.NoSave
	channel.SendOnlyChannel
}

func NewState(l *log.Logger, p *player.Player, q *collection.Queue) *StateChannel {
	return &StateChannel{log: l, p: p, q: q}
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
	state.Pos = c.p.Position()
	state.Duration = c.p.Duration()
	state.Song = ""
	state.Volume = c.p.Volume()

	cur := c.q.Current()
	s := cur.Song
	if s != nil {
		state.Song = s.Title()
	}
	if err := c.sender.Broadcast(f, state); err != nil {
		c.log.Println(err)
	}
}
