package history

import (
	"errors"
	"io"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/history/data"
)

type Log struct {
	From  channel.Client
	Stamp time.Time
	Msg   channel.Msg
	*channel.NeverEqual
}

func (l Log) Binary(w *binary.Writer) error {
	var b byte
	if l.From.Bot() {
		b = 1
	}
	w.WriteString(l.From.Name(), 8)
	w.WriteUint8(b)
	w.WriteUint64(uint64(l.Stamp.Unix()))
	return l.Msg.Binary(w)
}

func (l Log) JSON(r io.Writer) error { return errors.New("not implemented") }
func (l Log) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return l, errors.New("not implemented")
}

func (l Log) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return l, r, errors.New("not implemented")
}

type Output interface {
	FromHistory(to channel.Client, l Log) ([]channel.Batch, error)
	DecodeHistoryItem(*binary.Reader) (channel.Msg, error)
}

type HistoryChannel struct {
	amount int
	last   int

	*channel.BinaryHistory

	output  Output
	sender  channel.Sender
	channel string
	channel.Limit
}

func New(amountStore, amountSend int) *HistoryChannel {
	return &HistoryChannel{amount: amountStore, last: amountSend, Limit: channel.Limiter(255)}
}

func (c *HistoryChannel) Add(m channel.Msg) { panic("do not use add directly") }

func (c *HistoryChannel) AddLog(cl channel.Client, m channel.Msg) {
	c.BinaryHistory.Add(Log{From: cl, Msg: m, Stamp: time.Now()})
}

func (c *HistoryChannel) SetOutput(o Output) {
	if c.output != nil {
		panic("output already set")
	}

	c.BinaryHistory = channel.NewBinaryHistory(
		c.amount,
		"v2",
		map[channel.DecoderVersion]channel.Decoder{
			"v1": func(r *binary.Reader) (channel.Msg, error) {
				var l Log
				var err error
				l.From = channel.NewClient(r.ReadString(8), r.ReadUint8() == 1)
				l.Msg, err = c.output.DecodeHistoryItem(r)
				return l, err
			},
			"v2": func(r *binary.Reader) (channel.Msg, error) {
				var l Log
				var err error
				l.From = channel.NewClient(r.ReadString(8), r.ReadUint8() == 1)
				l.Stamp = time.Unix(int64(r.ReadUint64()), 0)
				l.Msg, err = c.output.DecodeHistoryItem(r)
				return l, err
			},
		},
	)
	c.output = o
}

func (c *HistoryChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *HistoryChannel) HandleBIN(cl channel.Client, r *binary.Reader) error {
	msg, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, msg)
}

func (c *HistoryChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	msg, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, msg)
}

func (c *HistoryChannel) handle(cl channel.Client, msg data.Message) error {
	var gerr error
	last := int(msg.Amount)
	if last > c.last {
		last = c.last
	}
	if last == 0 {
		return nil
	}

	b := make([]channel.Batch, 1)
	b[0] = channel.Batch{
		Filter: channel.ClientFilter{Channel: c.channel},
		Msg:    data.ServerMessage{},
	}

	c.BinaryHistory.Last(last, func(m channel.Msg) bool {
		bat, err := c.output.FromHistory(cl, m.(Log))
		if err != nil {
			gerr = err
			return false
		}
		b = append(b, bat...)
		return true
	})
	if gerr != nil {
		return gerr
	}
	for i := range b {
		b[i].Filter.Client = cl
	}

	return c.sender.BroadcastBatch(b)
}
