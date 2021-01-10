package channel

import (
	"bytes"
	"io"
	"log"

	"github.com/frizinak/binary"
)

type RWFactory interface {
	Reader(io.Reader) io.Reader
	Writer(io.Writer) io.Writer

	BinaryReader(io.Reader) BinaryReader
	BinaryWriter(io.Writer) BinaryWriter
}

type BinaryWriter interface {
	Err() error

	Writer() io.Writer

	WriteUint(uint64, byte)
	WriteUint8(uint8)
	WriteUint16(uint16)
	WriteUint32(uint32)
	WriteUint64(uint64)

	WriteBytes([]byte, byte)
	WriteString(string, byte)
}

type BinaryReader interface {
	Err() error

	Reader() io.Reader

	ReadUint(byte) uint64
	ReadUint8() uint8
	ReadUint16() uint16
	ReadUint32() uint32
	ReadUint64() uint64

	ReadBytes(byte) []byte
	ReadString(byte) string
}

func NewRWFactory(debug io.Writer) RWFactory {
	if debug != nil {
		return &debugRWFactory{
			log.New(debug, "< ", log.Ltime|log.Lmicroseconds),
			log.New(debug, "> ", log.Ltime|log.Lmicroseconds),
			&defaultRWFactory{},
		}
	}
	return &defaultRWFactory{}
}

type defaultRWFactory struct{}

func (d *defaultRWFactory) Reader(r io.Reader) io.Reader          { return r }
func (d *defaultRWFactory) Writer(w io.Writer) io.Writer          { return w }
func (d *defaultRWFactory) BinaryReader(r io.Reader) BinaryReader { return binary.NewReader(r) }
func (d *defaultRWFactory) BinaryWriter(w io.Writer) BinaryWriter { return binary.NewWriter(w) }

type debugRWFactory struct {
	logR, logW *log.Logger
	*defaultRWFactory
}

func (d *debugRWFactory) Reader(r io.Reader) io.Reader { return &debugR{d.logR, r} }
func (d *debugRWFactory) Writer(w io.Writer) io.Writer { return &debugW{d.logW, w} }

func ascii(d []byte) interface{} {
	if len(d) > 1024 {
		return nil
	}
	s := bytes.Trim(d, "\x00\n ")
	for _, n := range s {
		if n < 32 && n != '\n' && n != '\r' {
			return d
		}
	}
	return string(s)
}

type debugR struct {
	log *log.Logger
	r   io.Reader
}

func (d *debugR) Read(b []byte) (int, error) {
	n, err := d.r.Read(b)
	if err != nil {
		return n, err
	}
	if a := ascii(b[:n]); a != nil {
		d.log.Println(a)
	}
	return n, err
}

type debugW struct {
	log *log.Logger
	w   io.Writer
}

func (d *debugW) Write(b []byte) (int, error) {
	if a := ascii(b); a != nil {
		d.log.Println(a)
	}
	return d.w.Write(b)
}

// type debugBW struct {
// 	log *log.Logger
// 	w   BinaryWriter
// }
//
// func (d *debugBW) Err() error                   { return d.w.Err() }
// func (d *debugBW) Writer() io.Writer            { return d.w.Writer() }
// func (d *debugBW) WriteUint(a uint64, b byte)   { d.w.WriteUint(a, b); d.log.Println("--:", a) }
// func (d *debugBW) WriteUint8(a uint8)           { d.w.WriteUint8(a); d.log.Println("08:", a) }
// func (d *debugBW) WriteUint16(a uint16)         { d.w.WriteUint16(a); d.log.Println("16:", a) }
// func (d *debugBW) WriteUint32(a uint32)         { d.w.WriteUint32(a); d.log.Println("32:", a) }
// func (d *debugBW) WriteUint64(a uint64)         { d.w.WriteUint64(a); d.log.Println("64:", a) }
// func (d *debugBW) WriteBytes(a []byte, b byte)  { d.w.WriteBytes(a, b); d.log.Println("bt:", a) }
// func (d *debugBW) WriteString(a string, b byte) { d.w.WriteString(a, b); d.log.Println("st:", a) }

//type BinaryReader interface {
//	Err() error
//
//	Reader() io.Reader
//
//	ReadUint(byte) uint64
//	ReadUint8() uint8
//	ReadUint16() uint16
//	ReadUint32() uint32
//	ReadUint64() uint64
//
//	ReadBytes(byte) []byte
//	ReadString(byte) string
//}

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

type FlushFlusher struct {
	flushers []Flusher
}

func (w *FlushFlusher) Flush() error {
	for _, f := range w.flushers {
		if err := f.Flush(); err != nil {
			return err
		}
	}
	return nil
}

func NewFlushFlusher(flushers ...Flusher) Flusher {
	return &FlushFlusher{flushers}
}
