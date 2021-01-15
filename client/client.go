package client

import (
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

var ErrFingerPrint = errors.New("fingerprint mismatch")

type Backend interface {
	Connect() (Conn, error)
	Framed() bool
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

type RW struct {
	r    io.Reader
	w    channel.WriteFlusher
	conn io.Closer
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

	conn *RW

	lastConnect time.Time
	fatal       error

	serverFingerprint string

	c Config
}

type Config struct {
	Key *crypto.Key

	ServerURL         string
	ServerFingerprint string

	Name     string
	Channels []string
	Proto    channel.Proto
	History  uint16
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

func (c *Client) Users() Users                   { return c.users }
func (c *Client) Playlists() []string            { return c.playlists }
func (c *Client) Latency() time.Duration         { return c.latency }
func (c *Client) Name() string                   { return c.c.Name }
func (c *Client) Err() error                     { return c.fatal }
func (c *Client) ServerFingerprint() string      { return c.serverFingerprint }
func (c *Client) SetTrustedFingerprint(n string) { c.c.ServerFingerprint = n }

func (c *Client) Chat(msg string) error {
	return c.Send(vars.ChatChannel, chatdata.Message{Data: msg})
}

func (c *Client) Music(msg string) error {
	return c.Send(vars.MusicChannel, musicdata.Message{Command: msg})
}

func (c *Client) Send(chnl string, msg channel.Msg) error {
	_, w, err := c.tryConnect()
	if err != nil {
		c.disconnect()
		return err
	}

	c.sem.Lock()
	if err := c.writeMulti(w, channel.ChannelMsg{Data: chnl}, msg); err != nil {
		c.sem.Unlock()
		c.disconnect()
		return err
	}

	c.sem.Unlock()
	return nil
}

func (c *Client) Connect() error {
	c.fatal = nil
	_, err := c.connect()
	if err != nil {
		c.disconnect()
	}
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
		c.conn.conn.Close()
	}
	c.conn = nil
}

func (c *Client) tryConnect() (io.Reader, channel.WriteFlusher, error) {
	c.sem.Lock()
	defer c.sem.Unlock()
	conn := c.conn
	if conn != nil {
		return conn.r, conn.w, nil
	}

	for time.Since(c.lastConnect) < time.Second*2 {
		time.Sleep(time.Second)
		c.log.Log("reconnecting...")
	}

	c.lastConnect = time.Now()
	underlying, err := c.backend.Connect()
	if err != nil {
		return nil, nil, err
	}

	c.log.Log("connecting...")
	if err := c.negotiateProto(underlying); err != nil {
		return nil, nil, err
	}

	c.log.Log("encrypting...")
	fp, r, w, err := c.negotiateCrypto(underlying, underlying)
	if err != nil {
		return nil, nil, err
	}

	c.serverFingerprint = fp
	if c.c.ServerFingerprint == "" || c.c.ServerFingerprint != fp {
		c.fatal = ErrFingerPrint
		return nil, nil, ErrFingerPrint
	}

	if r, err = c.negotiateSymmetric(r, w); err != nil {
		return nil, nil, err
	}

	if r, err = c.negotiateUser(r, w); err != nil {
		return nil, nil, err
	}
	c.log.Log("connected")

	c.conn = &RW{r, w, underlying}
	return r, w, nil
}

func (c *Client) negotiateProto(w io.Writer) error {
	return c.c.Proto.Write(w)
}

func (c *Client) negotiateCrypto(r io.Reader, w io.Writer) (string, io.Reader, channel.WriteFlusher, error) {
	var wf channel.WriteFlusher = channel.NewPassthrough(w)
	if c.backend.Framed() {
		wf = channel.NewBuffered(w)
	}

	_server, nr, err := c.read(r, channel.PubKeyServerMessage{})
	if err != nil {
		return "", nil, nil, err
	}
	server := _server.(channel.PubKeyServerMessage)

	client, err := channel.NewPubKeyMessage(c.c.Key, server)
	if err != nil {
		return "", nil, nil, err
	}

	if err := c.write(wf, client); err != nil {
		return "", nil, nil, err
	}

	derive, err := channel.CommonSecret(client, server, nil)
	if err != nil {
		return "", nil, nil, err
	}

	rw := crypto.NewEncDec(
		nr,
		wf,
		derive(channel.CryptoClientRead),
		derive(channel.CryptoClientWrite),
		crypto.EncrypterConfig{SaltSize: 32, Cost: 12},
		crypto.DecrypterConfig{MinSaltSize: 32, MinCost: 12},
	)

	return server.Fingerprint(), rw, &channel.WriterFlusher{rw, wf}, nil
}

func (c *Client) negotiateSymmetric(r io.Reader, w channel.WriteFlusher) (io.Reader, error) {
	test, nr, err := c.read(r, channel.SymmetricTestMessage{})
	if err != nil {
		return nr, err
	}
	return nr, c.write(w, test)
}

func (c *Client) negotiateUser(r io.Reader, w channel.WriteFlusher) (io.Reader, error) {
	msg := channel.IdentifyMsg{Data: c.c.Name, Channels: c.c.Channels, Version: vars.ProtocolVersion}
	if err := c.write(w, msg); err != nil {
		return r, err
	}

	_status, nr, err := c.read(r, channel.StatusMsg{})
	if err != nil {
		return nr, err
	}

	s := _status.(channel.StatusMsg)
	if !s.OK() {
		err = errors.New(s.Err)
		if s.Is(channel.StatusUpdateClient) {
			suffix := ""
			if runtime.GOOS == "windows" {
				suffix = ".exe"
			}
			err = fmt.Errorf(
				"Client protocol does not match\nDownload new client: %s/clients/homechat-%s-%s%s",
				c.c.ServerURL,
				runtime.GOOS,
				runtime.GOARCH,
				suffix,
			)
		}
		c.fatal = err
		return nr, err
	}

	_identity, nr, err := c.read(nr, channel.IdentifyMsg{})
	if err != nil {
		return nr, err
	}

	identity := _identity.(channel.IdentifyMsg)
	c.c.Name = identity.Data
	c.handler.HandleName(c.c.Name)
	return nr, nil
}

func (c *Client) connect() (io.Reader, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}

	r, _, err := c.tryConnect()
	if err != nil {
		return r, err
	}
	if c.c.History > 0 {
		if err = c.Send(vars.HistoryChannel, historydata.New(c.c.History)); err != nil {
			return r, err
		}
	}

	return r, c.Send(vars.UserChannel, usersdata.Message{})
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

func (c *Client) writeMulti(w channel.WriteFlusher, ms ...channel.Msg) error {
	for _, m := range ms {
		if err := c.writeRaw(w, m); err != nil {
			return err
		}
	}
	return w.Flush()
}

func (c *Client) write(w channel.WriteFlusher, m channel.Msg) error {
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
	done := make(chan struct{}, 1)
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

	doOne := func(r io.Reader) (io.Reader, error) {
		var msg channel.Msg
		var err error

		msg, r, err = c.read(r, channel.ChannelMsg{})
		if err != nil {
			return r, err
		}
		chnl := msg.(channel.ChannelMsg)

		switch chnl.Data {
		case vars.PingChannel:
			c.latency = time.Since(<-pings)
			c.handler.HandleLatency(c.latency)
		case vars.HistoryChannel:
			_, r, err = c.read(r, historydata.ServerMessage{})
			if err != nil {
				return r, err
			}
			c.handler.HandleHistory()
		case vars.ChatChannel:
			msg, r, err = c.read(r, chatdata.ServerMessage{})
			if err != nil {
				return r, err
			}
			return r, c.handler.HandleChatMessage(msg.(chatdata.ServerMessage))
		case vars.MusicChannel:
			msg, r, err = c.read(r, musicdata.ServerMessage{})
			if err != nil {
				return r, err
			}
			return r, c.handler.HandleMusicMessage(msg.(musicdata.ServerMessage))
		case vars.MusicStateChannel:
			msg, r, err = c.read(r, musicdata.ServerStateMessage{})
			if err != nil {
				return r, err
			}
			musicState.ServerStateMessage = msg.(musicdata.ServerStateMessage)
			return r, c.handler.HandleMusicStateMessage(musicState)
		case vars.MusicSongChannel:
			msg, r, err = c.read(r, musicdata.ServerSongMessage{})
			if err != nil {
				return r, err
			}
			musicState.ServerSongMessage = msg.(musicdata.ServerSongMessage)
			return r, c.handler.HandleMusicStateMessage(musicState)
		case vars.MusicPlaylistChannel:
			msg, r, err = c.read(r, musicdata.ServerPlaylistMessage{})
			if err != nil {
				return r, err
			}

			c.playlists = msg.(musicdata.ServerPlaylistMessage).List
		case vars.UserChannel:
			msg, r, err = c.read(r, usersdata.ServerMessage{})
			if err != nil {
				return r, err
			}
			msg := msg.(usersdata.ServerMessage)
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
			return r, c.handler.HandleUsersMessage(msg, list)
		case vars.MusicErrorChannel:
			msg, r, err = c.read(r, channel.StatusMsg{})
			if err != nil {
				return r, err
			}
			msg := msg.(channel.StatusMsg)
			if msg.OK() {
				return r, nil
			}
			c.log.Flash(msg.Err)
		default:
			return r, fmt.Errorf("received unknown message type: '%s'", chnl)
		}

		return r, nil
	}

	do := func(r io.Reader) error {
		var err error
		for {
			r, err = doOne(r)
			if err != nil {
				return err
			}
		}
	}

	var gerr error
	for {
		r, err := c.connect()
		if err != nil {
			if err := c.Err(); err != nil {
				gerr = err
				break
			}
			c.disconnect()
			c.log.Err(err.Error())
			continue
		}

		if err := do(r); err != nil {
			c.disconnect()
			c.log.Err(err.Error())
			continue
		}
	}

	done <- struct{}{}
	return gerr
}
