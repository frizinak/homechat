package client

import (
	"errors"

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

	w            channel.WriteFlusher
	binaryWriter channel.BinaryWriter

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

func New(c Config, conn channel.WriteFlusher, binaryWriter channel.BinaryWriter, errs chan<- Error) *Client {
	return &Client{
		w:            conn,
		binaryWriter: binaryWriter,
		frameWriter:  c.FrameWriter,
		proto:        c.Proto,
		name:         c.Name,
		channels:     c.Channels,
		last:         make(map[string]channel.Msg),
		jobs:         make(chan Job, c.JobBuffer),
		errs:         errs,
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
	c.last[chnl] = msg

	p := channel.ChannelMsg{Data: chnl}
	switch c.proto {
	case channel.ProtoBinary:
		if err := p.Binary(c.binaryWriter); err != nil {
			return err
		}
		if err := msg.Binary(c.binaryWriter); err != nil {
			return err
		}
	case channel.ProtoJSON:
		if err := p.JSON(c.w); err != nil {
			return err
		}
		if err := msg.JSON(c.w); err != nil {
			return err
		}
	default:
		return errors.New("client uses unsupported protocol")
	}

	return c.w.Flush()
}

func (c *Client) Name() string       { return c.name }
func (c *Client) Channels() []string { return c.channels }
func (c *Client) Bot() bool          { return false }
