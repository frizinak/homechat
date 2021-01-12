package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/crypto"
	"github.com/frizinak/homechat/server/channel"
	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
	historydata "github.com/frizinak/homechat/server/channel/history/data"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	pingdata "github.com/frizinak/homechat/server/channel/ping/data"
	uploaddata "github.com/frizinak/homechat/server/channel/upload/data"
	usersdata "github.com/frizinak/homechat/server/channel/users/data"
	"github.com/frizinak/homechat/vars"
)

type Backend interface {
	Connect() (Conn, error)
}

type Handler interface {
	HandleName(name string)
	HandleHistory()
	HandleLatency(time.Duration)
	HandleChatMessage(chatdata.ServerMessage) error
	HandleMusicMessage(musicdata.ServerMessage) error
	HandleMusicStateMessage(MusicState) error
	HandleUsersMessage(usersdata.ServerMessage, Users) error
}

type User struct {
	Name     string
	Amount   uint8
	Channels []string
}

func (u User) String() string { return fmt.Sprintf("%s:%d", u.Name, u.Amount) }

type Users []User

func (u Users) Len() int           { return len(u) }
func (u Users) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }
func (u Users) Less(i, j int) bool { return u[i].Name < u[j].Name }
func (u Users) Names() []string {
	n := make([]string, len(u))
	for i, us := range u {
		n[i] = us.Name
	}
	return n
}

type Conn interface {
	io.Writer
	io.Reader
	io.Closer
}

type Logger interface {
	Log(string)
	Err(string)
	Flash(string)
}

type Client struct {
	log     Logger
	backend Backend
	handler Handler

	sem sync.Mutex

	users    Users
	allUsers map[string]map[string]User

	latency time.Duration

	playlists []string

	conn        Conn
	rw          io.ReadWriter
	lastConnect time.Time
	fatal       error

	c Config
}

type Config struct {
	ServerURL string
	Name      string
	Channels  []string
	Proto     channel.Proto
	Framed    bool
	History   uint16
}

func New(b Backend, h Handler, log Logger, c Config) *Client {
	return &Client{
		backend: b,
		handler: h,
		log:     log,
		c:       c,

		allUsers: make(map[string]map[string]User),
	}
}

func (c *Client) Users() Users           { return c.users }
func (c *Client) Playlists() []string    { return c.playlists }
func (c *Client) Latency() time.Duration { return c.latency }
func (c *Client) Name() string           { return c.c.Name }
func (c *Client) Err() error             { return c.fatal }

func (c *Client) Chat(msg string) error {
	return c.Send(vars.ChatChannel, chatdata.Message{Data: msg})
}

func (c *Client) Music(msg string) error {
	return c.Send(vars.MusicChannel, musicdata.Message{Command: msg})
}

func (c *Client) Send(chnl string, msg channel.Msg) error {
	conn, err := c.connect()
	if err != nil {
		c.disconnect()
		return err
	}

	c.sem.Lock()
	if err := c.writeMulti(conn, channel.ChannelMsg{Data: chnl}, msg); err != nil {
		c.sem.Unlock()
		c.disconnect()
		return err
	}

	c.sem.Unlock()
	return nil
}

func (c *Client) Connect() error {
	_, err := c.connect()
	return err
}

func (c *Client) Close() {
	c.disconnect()
}

func (c *Client) Upload(chnl, filename, msg string, r io.Reader) error {
	if err := c.Send(chnl, uploaddata.NewMessage(filename, msg, r)); err != nil {
		return err
	}
	c.disconnect()
	return nil
}

func (c *Client) disconnect() {
	c.sem.Lock()
	defer c.sem.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = nil
	c.rw = nil
}

func (c *Client) connect() (io.ReadWriter, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}

	c.sem.Lock()
	if c.rw != nil {
		rw := c.rw
		c.sem.Unlock()
		return rw, nil
	}

	for time.Since(c.lastConnect) < time.Second*2 {
		time.Sleep(time.Second)
		c.log.Log("reconnecting...")
	}

	c.lastConnect = time.Now()
	conn, err := c.backend.Connect()
	if err != nil {
		c.sem.Unlock()
		return nil, err
	}

	c.log.Log("connected")
	c.conn = conn
	wpass := []byte("test")
	rpass := []byte("test2")
	c.rw = crypto.NewEncDec(
		conn,
		conn,
		rpass,
		wpass,
		32,
		8,
	)

	if err := c.c.Proto.Write(conn); err != nil {
		c.sem.Unlock()
		return c.rw, err
	}

	msg := channel.IdentifyMsg{Data: c.c.Name, Channels: c.c.Channels, Version: vars.ProtocolVersion}
	if err := c.write(c.rw, msg); err != nil {
		c.sem.Unlock()
		return c.rw, err
	}

	_status, _, err := c.read(c.rw, channel.StatusMsg{})
	status := _status.(channel.StatusMsg)
	if err != nil || !status.OK() {
		c.fatal = err
		if err == nil {
			c.fatal = errors.New(status.Err)
			if status.Is(channel.StatusUpdateClient) {
				suffix := ""
				if runtime.GOOS == "windows" {
					suffix = ".exe"
				}
				c.fatal = fmt.Errorf(
					"Client protocol does not match\nDownload new client: %s/clients/homechat-%s-%s%s",
					c.c.ServerURL,
					runtime.GOOS,
					runtime.GOARCH,
					suffix,
				)
			}
		}
		c.sem.Unlock()
		return c.rw, c.fatal
	}
	_identity, _, err := c.read(c.rw, channel.IdentifyMsg{})
	identity := _identity.(channel.IdentifyMsg)
	if err != nil {
		c.sem.Unlock()
		return c.rw, err
	}
	c.c.Name = identity.Data
	c.handler.HandleName(c.c.Name)
	c.sem.Unlock()

	if c.c.History > 0 {
		if err = c.Send(vars.HistoryChannel, historydata.New(c.c.History)); err != nil {
			return c.rw, err
		}
	}

	if err = c.Send(vars.UserChannel, usersdata.Message{}); err != nil {
		return c.rw, err
	}

	return c.rw, nil
}

func (c *Client) writeRaw(w io.Writer, m channel.Msg) error {
	switch c.c.Proto {
	case channel.ProtoJSON:
		return m.JSON(w)
	case channel.ProtoBinary:
		return m.Binary(binary.NewWriter(w))
	default:
		return errors.New("invalid protocol")
	}
}

func (c *Client) writeMulti(w io.Writer, ms ...channel.Msg) error {
	rw := w
	var byw *bytes.Buffer
	if c.c.Framed {
		byw = bytes.NewBuffer(nil)
		rw = byw
	}

	for _, m := range ms {
		if err := c.writeRaw(rw, m); err != nil {
			return err
		}
	}

	if c.c.Framed {
		_, err := byw.WriteTo(w)
		return err
	}
	return nil
}

func (c *Client) write(w io.Writer, m channel.Msg) error {
	return c.writeMulti(w, m)
}

func (c *Client) read(r io.Reader, msg channel.Msg) (channel.Msg, io.Reader, error) {
	switch c.c.Proto {
	case channel.ProtoJSON:
		return msg.FromJSON(r)
	case channel.ProtoBinary:
		rr := binary.NewReader(r)
		m, err := msg.FromBinary(rr)
		return m, r, err
	default:
		return nil, r, errors.New("invalid protocol")
	}
}

func (c *Client) Run() error {
	hasPing := false
	for _, c := range c.c.Channels {
		if c == vars.PingChannel {
			hasPing = true
			break
		}
	}

	pings := make(chan time.Time, 1)
	done := make(chan struct{})
	go func() {
		for {
			if hasPing {
				pings <- time.Now()
			}
			if err := c.Send(vars.PingChannel, pingdata.Message{}); err != nil {
				c.log.Err(err.Error())
			}
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond * 2000):
			}
		}
	}()

	musicState := MusicState{}

	doOne := func(r io.Reader) error {
		_chnl, nr, err := c.read(r, channel.ChannelMsg{})
		if err != nil {
			return err
		}
		chnl := _chnl.(channel.ChannelMsg)
		r = nr

		switch chnl.Data {
		case vars.PingChannel:
			c.latency = time.Since(<-pings)
			c.handler.HandleLatency(c.latency)
		case vars.HistoryChannel:
			_, _, err := c.read(r, historydata.ServerMessage{})
			if err != nil {
				return err
			}
			c.handler.HandleHistory()
		case vars.ChatChannel:
			_msg, _, err := c.read(r, chatdata.ServerMessage{})
			if err != nil {
				return err
			}
			return c.handler.HandleChatMessage(_msg.(chatdata.ServerMessage))
		case vars.MusicChannel:
			_msg, _, err := c.read(r, musicdata.ServerMessage{})
			if err != nil {
				return err
			}
			return c.handler.HandleMusicMessage(_msg.(musicdata.ServerMessage))
		case vars.MusicStateChannel:
			_msg, _, err := c.read(r, musicdata.ServerStateMessage{})
			if err != nil {
				return err
			}
			musicState.ServerStateMessage = _msg.(musicdata.ServerStateMessage)
			return c.handler.HandleMusicStateMessage(musicState)
		case vars.MusicSongChannel:
			_msg, _, err := c.read(r, musicdata.ServerSongMessage{})
			if err != nil {
				return err
			}
			musicState.ServerSongMessage = _msg.(musicdata.ServerSongMessage)
			return c.handler.HandleMusicStateMessage(musicState)
		case vars.MusicPlaylistChannel:
			_msg, _, err := c.read(r, musicdata.ServerPlaylistMessage{})
			if err != nil {
				return err
			}

			c.playlists = _msg.(musicdata.ServerPlaylistMessage).List
		case vars.UserChannel:
			_msg, _, err := c.read(r, usersdata.ServerMessage{})
			if err != nil {
				return err
			}
			msg := _msg.(usersdata.ServerMessage)
			users := make(map[string]User, len(msg.Users))
			for _, u := range msg.Users {
				users[u.Name] = User{Name: u.Name, Amount: u.Clients}
			}

			c.allUsers[msg.Channel] = users

			list := make(Users, 0, len(users))
			v := make(map[string]struct{}, len(users))
			for _, ch := range c.c.Channels {
				us, ok := c.allUsers[ch]
				if !ok {
					continue
				}

				for _, user := range us {
					for dch := range c.allUsers {
						if _, ok := c.allUsers[dch][user.Name]; !ok {
							continue
						}
						user.Channels = append(user.Channels, dch)
					}
					if _, ok := v[user.Name]; ok {
						continue
					}
					v[user.Name] = struct{}{}
					list = append(list, user)
				}
			}

			sort.Sort(list)
			c.users = list
			return c.handler.HandleUsersMessage(msg, list)
		case vars.MusicErrorChannel:
			_msg, _, err := c.read(r, channel.StatusMsg{})
			if err != nil {
				return err
			}
			msg := _msg.(channel.StatusMsg)
			if msg.OK() {
				return nil
			}
			c.log.Flash(msg.Err)
		default:
			return fmt.Errorf("received unknown message type: '%s'", chnl)
		}

		return nil
	}

	do := func(r io.Reader) error {
		for {
			err := doOne(r)
			if err != nil {
				return err
			}
		}
	}

	var gerr error
	for {
		conn, err := c.connect()
		if err != nil {
			if err := c.Err(); err != nil {
				gerr = err
				break
			}
			c.disconnect()
			c.log.Err(err.Error())
			continue
		}

		if err := do(conn); err != nil {
			c.disconnect()
			c.log.Err(err.Error())
			continue
		}
	}

	done <- struct{}{}
	return gerr
}
