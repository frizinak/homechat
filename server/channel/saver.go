package channel

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/frizinak/binary"
)

type Saver interface {
	Add(d Msg)
	Each(func(Msg) bool)
	Last(int, func(Msg) bool)
}

type Decoder = func(r BinaryReader) (Msg, error)

type DecoderVersion string

type BinaryHistory struct {
	sem     sync.RWMutex
	data    []Msg
	dec     map[DecoderVersion]Decoder
	max     int
	haveNew bool

	appendOnlyFile   io.Closer
	appendOnlyWriter BinaryWriter
	app              chan Msg
	appending        bool
	done             chan struct{}

	current DecoderVersion
}

func NewBinaryHistory(
	max int,
	appendOnlyFile string,
	current DecoderVersion,
	dec map[DecoderVersion]Decoder,
) (*BinaryHistory, error) {
	b := &BinaryHistory{
		data:    make([]Msg, 0),
		max:     max,
		dec:     dec,
		current: current,
	}

	if appendOnlyFile != "" {
		f, err := os.OpenFile(appendOnlyFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return b, fmt.Errorf("Could not open append only file %s: %w", appendOnlyFile, err)
		}
		b.appendOnlyFile = f
		b.appendOnlyWriter = binary.NewWriter(f)
		b.app = make(chan Msg, max)
	}

	return b, nil
}

func (g *BinaryHistory) StartAppend() error {
	if g.app == nil {
		return nil
	}
	g.done = make(chan struct{}, 1)
	g.appending = true
	g.appendOnlyWriter.WriteString(string(g.current), 16)
	defer func() {
		g.appendOnlyFile.Close()
		g.done <- struct{}{}
	}()

	for m := range g.app {
		if err := m.Binary(g.appendOnlyWriter); err != nil {
			return err
		}
		if err := g.appendOnlyWriter.Err(); err != nil {
			return err
		}
	}

	return nil
}

func (g *BinaryHistory) StopAppend() {
	if !g.appending {
		return
	}
	g.appending = false
	close(g.app)
	<-g.done
}

func (g *BinaryHistory) DecodeAppendFile(r io.Reader, cb func(Msg)) error {
	peeker := bufio.NewReader(r)
	bin := binary.NewReader(peeker)
	v := DecoderVersion(bin.ReadString(16))
	if err := bin.Err(); err != nil {
		return err
	}

	dec, ok := g.dec[v]
	if !ok {
		return fmt.Errorf("no decoder for version '%s'", v)
	}

	for {
		_, err := peeker.Peek(1)
		if err == io.EOF {
			return bin.Err()
		}
		m, err := dec(bin)
		if err != nil {
			return err
		}
		cb(m)
	}
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

	do := func(r BinaryReader, dec Decoder) error {
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
	if g.appending {
		g.app <- d
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
