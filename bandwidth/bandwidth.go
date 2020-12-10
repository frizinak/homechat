package bandwidth

import (
	"io"
	"sync"
	"time"
)

type Reader struct {
	n uint64
	r io.Reader
}

func (r *Reader) Read(b []byte) (int, error) {
	n, err := r.r.Read(b)
	r.n += uint64(n)
	return n, err
}

type Writer struct {
	n uint64
	w io.Writer
}

func (w *Writer) Write(b []byte) (int, error) {
	n, err := w.w.Write(b)
	w.n += uint64(n)
	return n, err
}

type Tracker struct {
	sem     sync.RWMutex
	readers []*Reader
	writers []*Writer

	lastUp    uint64
	lastDown  uint64
	lastStamp time.Time
}

func New() *Tracker {
	return &Tracker{
		readers:   make([]*Reader, 0),
		writers:   make([]*Writer, 0),
		lastStamp: time.Now(),
	}
}

func (b *Tracker) NewReader(r io.Reader) io.Reader {
	b.sem.Lock()
	rr := &Reader{0, r}
	b.readers = append(b.readers, rr)
	b.sem.Unlock()
	return rr
}

func (b *Tracker) NewWriter(w io.Writer) io.Writer {
	b.sem.Lock()
	ww := &Writer{0, w}
	b.writers = append(b.writers, ww)
	b.sem.Unlock()
	return ww
}

func (b *Tracker) Get() (up, down float64) {
	b.sem.RLock()
	var tdown, tup uint64
	for _, r := range b.readers {
		tdown += r.n
	}
	for _, w := range b.writers {
		tup += w.n
	}

	now := time.Now()
	since := now.Sub(b.lastStamp).Seconds()
	b.lastStamp = now
	b.sem.RUnlock()

	up = float64(tup-b.lastUp) / since
	down = float64(tdown-b.lastDown) / since
	b.lastUp = tup
	b.lastDown = tdown
	return
}
