package channel

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/frizinak/binary"
)

type Saver interface {
	Add(d Msg)
	Each(func(Msg) bool)
	Last(int, func(Msg) bool)
}

type Decoder = func(r *binary.Reader) (Msg, error)

type NoSave struct{}

type DecoderVersion string

func (ns NoSave) NeedsSave() bool        { return false }
func (ns NoSave) Save(file string) error { return errors.New("not implemented") }
func (ns NoSave) Load(file string) error { return nil }

type BinaryHistory struct {
	sem     sync.RWMutex
	data    []Msg
	dec     map[DecoderVersion]Decoder
	max     int
	haveNew bool

	current DecoderVersion
}

func NewBinaryHistory(max int, current DecoderVersion, dec map[DecoderVersion]Decoder) *BinaryHistory {
	return &BinaryHistory{data: make([]Msg, 0), max: max, dec: dec, current: current}
}

func (g *BinaryHistory) NeedsSave() bool { return g.haveNew }

func (g *BinaryHistory) Save(file string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()

	g.sem.Lock()
	defer g.sem.Unlock()
	w := binary.NewWriter(f)
	w.WriteString(string(g.current), 16)
	w.WriteUint64(uint64(len(g.data)))
	for _, m := range g.data {
		if err := m.Binary(w); err != nil {
			return err
		}
	}

	g.haveNew = false

	return w.Err()
}

func (g *BinaryHistory) Load(file string) error {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	do := func(r *binary.Reader, dec Decoder) error {
		n := r.ReadUint64()
		g.data = make([]Msg, 0, n)

		var i uint64
		for i = 0; i < n; i++ {
			m, err := dec(r)
			if err != nil {
				return err
			}

			g.data = append(g.data, m)
		}

		return r.Err()
	}

	g.sem.Lock()
	defer g.sem.Unlock()
	r := binary.NewReader(f)
	v := DecoderVersion(r.ReadString(16))

	if v != g.current {
		g.haveNew = true
	}

	dec, ok := g.dec[v]
	if !ok {
		return fmt.Errorf("no decoder for version '%s'", v)
	}

	return do(r, dec)
}

func (g *BinaryHistory) Add(d Msg) {
	g.sem.Lock()
	defer g.sem.Unlock()
	g.haveNew = true
	g.data = append(g.data, d)
	if len(g.data) > g.max {
		g.data = g.data[len(g.data)-g.max:]
	}
}

func (g *BinaryHistory) Each(cb func(Msg) bool) {
	g.sem.Lock()
	defer g.sem.Unlock()
	g.each(g.data, cb)
}

func (g *BinaryHistory) Last(n int, cb func(Msg) bool) {
	if n < 0 {
		return
	}

	g.sem.Lock()
	defer g.sem.Unlock()
	l := len(g.data) - n
	if l < 0 {
		l = 0
	}

	g.each(g.data[l:], cb)
}

func (g *BinaryHistory) each(d []Msg, cb func(Msg) bool) {
	for _, el := range d {
		if !cb(el) {
			break
		}
	}
}
