package bandwidth

import (
	"io"
)

type Bandwidth interface {
	NewReader(io.Reader) io.Reader
	NewWriter(io.Writer) io.Writer
	Get() (up, down float64, totalUp, totalDown uint64)
}

type Noop struct{}

func (n *Noop) NewReader(r io.Reader) io.Reader            { return r }
func (n *Noop) NewWriter(w io.Writer) io.Writer            { return w }
func (n *Noop) Get() (up, down float64, tup, tdown uint64) { return }
