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

type NeverEqual struct{}

func (n NeverEqual) Equal(Msg) bool { return false }

type NilMsg struct{ NeverEqual }

func (m NilMsg) Binary(w *binary.Writer) error                { return w.Err() }
func (m NilMsg) JSON(w io.Writer) error                       { return nil }
func (m NilMsg) FromBinary(r *binary.Reader) (Msg, error)     { return BinaryNilMessage(r) }
func (m NilMsg) FromJSON(r io.Reader) (Msg, io.Reader, error) { return JSONNilMessage(r) }
func BinaryNilMessage(r *binary.Reader) (m NilMsg, err error) { return }
func JSONNilMessage(r io.Reader) (NilMsg, io.Reader, error)   { return NilMsg{}, r, nil }
