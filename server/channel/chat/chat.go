package chat

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/bot"
	"github.com/frizinak/homechat/server/channel"
	"github.com/frizinak/homechat/server/channel/chat/data"
	"github.com/frizinak/homechat/server/channel/history"
)

var (
	reMention         = regexp.MustCompile(` @([^\s]+)`)
	reMentionSuffixes = regexp.MustCompile(`[/!\.\?\(\)\[\]]`)
	multiSpaceRE      = regexp.MustCompile(`\s+`)
)

const serverBot = "server-bot"

type ChatChannel struct {
	log  *log.Logger
	hist *history.HistoryChannel

	sender  channel.Sender
	channel string
	bots    *bot.BotCollection

	channel.NoSave
	channel.Limit
}

func New(log *log.Logger, hist *history.HistoryChannel) *ChatChannel {
	return &ChatChannel{
		log:   log,
		bots:  bot.NewBotCollection(serverBot),
		hist:  hist,
		Limit: channel.Limiter(1024 * 1024 * 5),
	}
}

func (c *ChatChannel) Register(chnl string, s channel.Sender) error {
	c.channel = chnl
	c.sender = s
	return nil
}

func (c *ChatChannel) AddBot(cmd string, bot bot.Bot) {
	c.bots.AddBot(cmd, bot)
}

func (c *ChatChannel) HandleBIN(cl channel.Client, r *binary.Reader) error {
	m, err := data.BinaryMessage(r)
	if err != nil {
		return err
	}
	return c.handle(cl, m)
}

func (c *ChatChannel) HandleJSON(cl channel.Client, r io.Reader) (io.Reader, error) {
	m, nr, err := data.JSONMessage(r)
	if err != nil {
		return nr, err
	}
	return nr, c.handle(cl, m)
}

func (c *ChatChannel) UserUpdate(cl channel.Client, r channel.ConnectionReason) error {
	// @see 9d037f8
	return nil
}

func (c *ChatChannel) FromHistory(to channel.Client, l history.Log) ([]channel.Batch, error) {
	msg := l.Msg.(data.Message)
	_b := c.batch(data.NotifyNever, l.From, msg)
	b := make([]channel.Batch, 0, len(_b))
	for _, bat := range _b {
		f := bat.Filter
		if !f.CheckIdentityAndName(to) {
			continue
		}
		m := bat.Msg.(data.ServerMessage)
		m.Stamp = l.Stamp
		bat.Msg = m
		b = append(b, bat)
	}

	return b, nil
}

func (c *ChatChannel) DecodeHistoryItem(r *binary.Reader) (channel.Msg, error) {
	return data.BinaryMessage(r)
}

func (c *ChatChannel) handle(cl channel.Client, m data.Message) error {
	c.hist.AddLog(cl, m)
	b := c.batch(data.NotifyDefault, cl, m)

	var gerr error
	for _, bat := range b {
		if err := c.sender.Broadcast(bat.Filter, bat.Msg); err != nil {
			gerr = err
		}
	}

	if len(m.Data) == 0 || (m.Data[0] != ':' && m.Data[0] != '/') {
		return gerr
	}

	go func() {
		if err := c.botMessage(cl, m); err != nil {
			c.log.Println("bot err", err)
		}
	}()

	return gerr
}

func (c *ChatChannel) botMessage(cl channel.Client, m data.Message) error {
	silent := m.Data[0] == ':'
	cmd := multiSpaceRE.Split(channel.StripUnprintable(m.Data[1:]), -1)
	name, d, err := c.bots.Message(cl.Name(), cmd...)
	if err == bot.ErrNotExists {
		return nil
	}

	if err != nil {
		return err
	}

	if name == "" {
		name = "unknown-bot"
	}

	if d == "" {
		return nil
	}

	if silent {
		d = fmt.Sprintf("@%s \n%s", cl.Name(), d)
	}

	return c.handle(channel.NewBot(name), data.Message{Data: d})
}

func (c *ChatChannel) batch(notify data.Notify, cl channel.Client, m data.Message) []channel.Batch {
	b := make([]channel.Batch, 0, 1)
	var f channel.ClientFilter
	f.Channel = c.channel

	fromBot := cl.Bot()
	if fromBot {
		notify |= data.NotifyNever
	}

	msg := channel.StripUnprintable(m.Data)
	s := data.ServerMessage{
		From:    cl.Name(),
		Stamp:   time.Now(),
		Message: data.Message{Data: msg},
		Bot:     fromBot,
		Notify:  notify,
	}

	if len(s.Data) > 0 && s.Data[0] == ':' {
		f.To = []string{cl.Name()}
		b = append(b, channel.Batch{f, s})
		return b
	}

	if len(s.Data) > 0 && s.Data[0] == '!' {
		s.Notify = notify | data.NotifyPersonal
	}

	if len(s.Data) > 0 && s.Data[0] == '@' {
		p := strings.SplitN(s.Data, " ", 2)
		if len(p[0]) != 1 {
			f.To = []string{p[0][1:]}
			s.Data = ""
			if len(p) == 2 {
				s.Data = p[1]
			}
			s.PM = p[0][1:]
			s.Notify = notify | data.NotifyPersonal
			b = append(b, channel.Batch{f, s})

			f.To = []string{s.From}
			s.Notify = notify | data.NotifyNever
			b = append(b, channel.Batch{f, s})

			return b
		}
	}

	mentions := reMention.FindAllStringSubmatch(s.Data, -1)
	if len(mentions) > 0 {
		mentionNames := make([]string, 0, len(mentions))
		for i := range mentions {
			mentionNames = append(mentionNames, mentions[i][1])
			p := reMentionSuffixes.Split(mentions[i][1], 2)
			if len(p) > 1 && len(p[0]) > 0 {
				mentionNames = append(
					mentionNames,
					p[0],
				)
			}
		}

		f.To = mentionNames
		s.Notify = notify | data.NotifyPersonal
		b = append(b, channel.Batch{f, s})

		f.NotTo = mentionNames
		f.To = nil
		s.Notify = notify | data.NotifyNever
		b = append(b, channel.Batch{f, s})
		return b
	}

	b = append(b, channel.Batch{f, s})
	return b
}
