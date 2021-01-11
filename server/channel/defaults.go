package channel

import (
	"errors"
	"io"

	"github.com/frizinak/binary"
)

type Limit struct {
	max int64
}

func (l Limit) LimitReader() int64 { return l.max }
func Limiter(n int64) Limit        { return Limit{n} }

type NoClose struct{}

func (n NoClose) Close() error { return nil }

type NoRun struct{}

func (n NoRun) Run() error { return nil }

type NoRunClose struct {
	NoRun
	NoClose
}
type SendOnly struct{}

func (s SendOnly) LimitReader() int64 { return 0 }
func (s SendOnly) HandleBIN(cl Client, r *binary.Reader) error {
	return errors.New("this channel can not receive messages")
}

func (s SendOnly) HandleJSON(cl Client, r io.Reader) (io.Reader, error) {
	return r, errors.New("this channel can not receive messages")
}

type NoSave struct{}

func (ns NoSave) NeedsSave() bool        { return false }
func (ns NoSave) Save(file string) error { return errors.New("not implemented") }
func (ns NoSave) Load(file string) error { return nil }
