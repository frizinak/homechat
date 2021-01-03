package channel

import (
	"errors"
	"io"
	"net/url"

	"github.com/frizinak/binary"
)

type Channel interface {
	Register(name string, s Sender) error
	NeedsSave() bool
	Save(string) error
	Load(file string) error

	LimitReader() int64

	HandleBIN(Client, *binary.Reader) error
	HandleJSON(Client, io.Reader) (io.Reader, error)
}

type Limit struct {
	max int64
}

func (l Limit) LimitReader() int64 { return l.max }
func Limiter(n int64) Limit        { return Limit{n} }

type SendOnlyChannel struct {
}

func (s SendOnlyChannel) LimitReader() int64 { return 0 }
func (s SendOnlyChannel) HandleBIN(cl Client, r *binary.Reader) error {
	return errors.New("this channel can not receive messages")
}
func (s SendOnlyChannel) HandleJSON(cl Client, r io.Reader) (io.Reader, error) {
	return r, errors.New("this channel can not receive messages")
}

type Sender interface {
	Broadcast(ClientFilter, Msg) error
	//BroadcastError(ClientFilter, error) error
	BroadcastBatch([]Batch) error
}

type Uploader interface {
	Upload(string, io.Reader) (*url.URL, error)
}

type User struct {
	Name    string
	Clients int
}

type UserCollection interface {
	GetUsers(ch string) []User
}

type ConnectionReason byte

const (
	Connect ConnectionReason = iota
	Disconnect
)

type UserUpdateHandler interface {
	UserUpdate(Client, ConnectionReason) error
}

type multiUserUpdateHandler struct {
	handlers []UserUpdateHandler
}

func MultiUserUpdateHandler(handlers ...UserUpdateHandler) UserUpdateHandler {
	return &multiUserUpdateHandler{handlers}
}

func (m *multiUserUpdateHandler) UserUpdate(c Client, r ConnectionReason) error {
	for _, h := range m.handlers {
		if err := h.UserUpdate(c, r); err != nil {
			return err
		}
	}

	return nil
}

type Batch struct {
	Filter ClientFilter
	Msg    Msg
}

type ClientFilter struct {
	Client     Client
	Channel    string
	HasChannel []string
	To         []string
	NotTo      []string
}

func (f ClientFilter) CheckName(n string) bool {
	for _, no := range f.NotTo {
		if n == no {
			return false
		}
	}

	found := len(f.To) == 0
	for _, yes := range f.To {
		if n == yes {
			found = true
		}
	}

	return found
}

func (f ClientFilter) CheckChannels(n []string) bool {
	if len(f.HasChannel) == 0 {
		return true
	}

	m := make(map[string]struct{}, len(n))
	for _, c := range n {
		m[c] = struct{}{}
	}

	for _, c := range f.HasChannel {
		if _, ok := m[c]; !ok {
			return false
		}
	}

	return true
}

func (f ClientFilter) CheckIdentity(c Client) bool {
	return f.Client == nil || f.Client == c
}

func (f ClientFilter) CheckIdentityAndName(c Client) bool {
	if !f.CheckIdentity(c) {
		return false
	}
	return f.CheckName(c.Name())
}

type Client interface {
	Name() string
	Bot() bool
}

type NameOnlyClient struct {
	name string
	bot  bool
}

func (n *NameOnlyClient) Name() string                { return n.name }
func (n *NameOnlyClient) Bot() bool                   { return n.bot }
func NewUser(name string) *NameOnlyClient             { return NewClient(name, false) }
func NewBot(name string) *NameOnlyClient              { return NewClient(name, true) }
func NewClient(name string, bot bool) *NameOnlyClient { return &NameOnlyClient{name, bot} }
