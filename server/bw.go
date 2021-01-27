package server

import (
	"io"
)

type Bandwidth interface {
	NewReader(io.Reader) io.Reader
	NewWriter(io.Writer) io.Writer
	Get() (up, down float64, totalUp, totalDown uint64)
}

type NoopBandwidth struct{}

func (n *NoopBandwidth) NewReader(r io.Reader) io.Reader            { return r }
func (n *NoopBandwidth) NewWriter(w io.Writer) io.Writer            { return w }
func (n *NoopBandwidth) Get() (up, down float64, tup, tdown uint64) { return }
