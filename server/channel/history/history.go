package history

import (
	"errors"
	"io"
	"log"
	"time"

	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/history/data"
)

type Log struct {
	From  channel.Client
	Stamp time.Time
	Msg   channel.Msg
	channel.NeverEqual
	channel.NoClose
}

func (l Log) Binary(w channel.BinaryWriter) error {
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
func (l Log) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	return l, errors.New("not implemented")
}

func (l Log) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return l, r, errors.New("not implemented")
}

type Output interface {
	FromHistory(to channel.Client, l Log) ([]channel.Batch, error)
	DecodeHistoryItem(channel.BinaryReader) (channel.Msg, error)
}

type HistoryChannel struct {
	log            *log.Logger
	amount         int
	appendOnlyFile string

	*channel.BinaryHistory

	output  Output
	sender  channel.Sender
	channel string
	channel.Limit
}

func New(log *log.Logger, amount int, appendOnlyFile string, o Output) (*HistoryChannel, error) {
	bin, err := channel.NewBinaryHistory(
		amount,
		appendOnlyFile,
		"v2",
		map[channel.DecoderVersion]channel.Decoder{
			"v1": func(r channel.BinaryReader) (channel.Msg, error) {
				var l Log
				var err error
				l.From = channel.NewClient(r.ReadString(8), r.ReadUint8() == 1)
				l.Msg, err = o.DecodeHistoryItem(r)
				return l, err
			},
			"v2": func(r channel.BinaryReader) (channel.Msg, error) {
				var l Log
				var err error
				l.From = channel.NewClient(r.ReadString(8), r.ReadUint8() == 1)
				l.Stamp = time.Unix(int64(r.ReadUint64()), 0)
				l.Msg, err = o.DecodeHistoryItem(r)
				return l, err
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return &HistoryChannel{
		log:            log,
		amount:         amount,
		appendOnlyFile: appendOnlyFile,
		Limit:          channel.Limiter(255),
		output:         o,
		BinaryHistory:  bin,
	}, nil
}

func (c *HistoryChannel) Add(m channel.Msg) { panic("do not use add directly") }

func (c *HistoryChannel) AddLog(cl channel.Client, m channel.Msg) {
	c.BinaryHistory.Add(Log{From: cl, Msg: m, Stamp: time.Now()})
}

func (c *HistoryChannel) Close() error {
	if c.BinaryHistory == nil {
		return nil
	}
	c.BinaryHistory.StopAppend()
	return nil
}

func (c *HistoryChannel) Run() error {
	return c.BinaryHistory.StartAppend()
}

func (c *HistoryChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *HistoryChannel) HandleBIN(cl channel.Client, r channel.BinaryReader) error {
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
	if last > c.amount {
		last = c.amount
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
