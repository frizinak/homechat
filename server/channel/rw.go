package channel

import (
	"bytes"
	"io"
)

type Flusher interface {
	Flush() error
}

type WriteFlusher interface {
	io.Writer
	Flusher
}

type BufferedWriteFlusher struct {
	w   io.Writer
	buf *bytes.Buffer
}

func NewBuffered(w io.Writer) WriteFlusher {
	return &BufferedWriteFlusher{w, bytes.NewBuffer(nil)}
}

func (w *BufferedWriteFlusher) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *BufferedWriteFlusher) Flush() error {
	_, err := w.buf.WriteTo(w.w)
	return err
}

type PassthroughWriteFlusher struct {
	io.Writer
}

func NewPassthrough(w io.Writer) WriteFlusher {
	return &PassthroughWriteFlusher{w}
}

func (w *PassthroughWriteFlusher) Flush() error { return nil }

type WriterFlusher struct {
	io.Writer
	Flusher
}
