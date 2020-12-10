package server

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type client struct {
	sem         sync.Mutex
	proto       channel.Proto
	frameWriter bool

	w io.Writer

	name     string
	channels []string

	last map[string]channel.Msg
}

func (c *client) msg(chnl string, msg channel.Msg) error {
	c.sem.Lock()
	defer c.sem.Unlock()

	last, ok := c.last[chnl]
	if ok && msg.Equal(last) {
		return nil
	}

	w := c.w
	var buf *bytes.Buffer
	if c.frameWriter {
		buf = bytes.NewBuffer(nil)
		w = buf
	}

	p := channel.ChannelMsg{Data: chnl}
	switch c.proto {
	case channel.ProtoBinary:
		wr := binary.NewWriter(w)
		if err := p.Binary(wr); err != nil {
			return err
		}
		if err := msg.Binary(wr); err != nil {
			return err
		}
	case channel.ProtoJSON:
		if err := p.JSON(w); err != nil {
			return err
		}
		if err := msg.JSON(w); err != nil {
			return err
		}
	default:
		return errors.New("client uses unsupported protocol")
	}

	if buf != nil {
		if _, err := c.w.Write(buf.Bytes()); err != nil {
			return err
		}
	}

	c.last[chnl] = msg

	return nil
}

func (c *client) Name() string { return c.name }
func (c *client) Bot() bool    { return false }
