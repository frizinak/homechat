package server

import (
	"fmt"
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

type Unit int

const (
	B   Unit = 1
	KiB      = B * 1024
	MiB      = KiB * 1024
	GiB      = MiB * 1024
)

func (u Unit) String() string {
	switch u {
	case B:
		return "B"
	case KiB:
		return "KiB"
	case MiB:
		return "MiB"
	case GiB:
		return "GiB"
	}

	return "?"
}

func (u Unit) Format() string {
	switch u {
	case B:
		return "%.0f"
	case KiB:
		return "%.2f"
	case MiB:
		return "%.2f"
	case GiB:
		return "%.1f"
	}

	return "%.2f"
}

var (
	order     = []Unit{B, KiB, MiB, GiB}
	ZeroBytes = NewBytes(0, B)
)

type Bytes struct {
	value float64
	unit  Unit
}

func (b Bytes) Human() Bytes {
	if b.unit >= GiB {
		return b
	}

	n := b.value * float64(b.unit)
	i := 0
	for n > 1024 && order[i] < GiB {
		n /= 1024
		i++
	}

	return NewBytes(n, order[i])
}

func (b Bytes) Unit() Unit {
	return b.unit
}

func (b Bytes) Convert(unit Unit) Bytes {
	b.value = b.value * (float64(b.unit) / float64(unit))
	b.unit = unit
	return b
}

func (b Bytes) String() string {
	format := fmt.Sprintf("%s%s", b.unit.Format(), b.unit.String())
	return fmt.Sprintf(format, b.value)
}

func (b Bytes) StringNoUnit() string {
	return fmt.Sprintf(b.unit.Format(), b.value)
}

func NewBytes(value float64, unit Unit) Bytes {
	return Bytes{value, unit}
}
