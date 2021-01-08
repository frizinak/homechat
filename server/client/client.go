package client

import (
	"bytes"
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type Job struct {
	Channel string
	Msgs    []channel.Msg
}

type Error struct {
	Client *Client
	Err    error
}

type Client struct {
	proto       channel.Proto
	frameWriter bool

	w io.Writer

	name     string
	channels []string

	last map[string]channel.Msg

	jobs chan Job
	errs chan<- Error

	stopped bool
}

type Config struct {
	FrameWriter bool
	Proto       channel.Proto
	Name        string
	Channels    []string
	JobBuffer   int
}

func New(c Config, conn io.Writer, errs chan<- Error) *Client {
	return &Client{
		w:           conn,
		frameWriter: c.FrameWriter,
		proto:       c.Proto,
		name:        c.Name,
		channels:    c.Channels,
		last:        make(map[string]channel.Msg),
		jobs:        make(chan Job, c.JobBuffer),
		errs:        errs,
	}
}

func (c *Client) Run() {
	go func() {
		for j := range c.jobs {
			for _, m := range j.Msgs {
				if err := c.send(j.Channel, m); err != nil {
					c.errs <- Error{c, err}
				}
			}
		}
	}()
}

func (c *Client) Stop() {
	c.stopped = true
	close(c.jobs)
}

func (c *Client) Queue(job Job) {
	if c.stopped {
		c.errs <- Error{c, errors.New("client was stopped but still received a message")}
		return
	}
	c.jobs <- job
}

func (c *Client) send(chnl string, msg channel.Msg) error {
	if last, ok := c.last[chnl]; ok && msg.Equal(last) {
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

func (c *Client) Name() string       { return c.name }
func (c *Client) Channels() []string { return c.channels }
func (c *Client) Bot() bool          { return false }
