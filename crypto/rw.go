package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"hash"
	"io"
)

type HMACWriter struct {
	hmac   hash.Hash
	w      io.Writer
	buf    *bytes.Buffer
	maxLen uint16
	b      []byte
}

func NewHMACWriter(w io.Writer, h func() hash.Hash, secret []byte, buffer uint16) *HMACWriter {
	return &HMACWriter{
		hmac.New(h, secret),
		w,
		bytes.NewBuffer(make([]byte, 0, buffer)),
		buffer,
		make([]byte, 2),
	}
}

func NewSHA1HMACWriter(w io.Writer, secret []byte, buffer uint16) *HMACWriter {
	return NewHMACWriter(w, sha1.New, secret, buffer)
}

func (h *HMACWriter) Write(b []byte) (int, error) {
	cut := int(h.maxLen) - h.buf.Len()
	if cut < 0 {
		panic("too small todo remove")
	}
	if cut > len(b) {
		cut = len(b)
	}

	n := b[:cut]
	wr, _ := h.buf.Write(n)
	h.hmac.Write(n)

	if cut == 0 {
		if err := h.Flush(); err != nil {
			return wr, err
		}
	}

	if cut == len(b) {
		return wr, nil
	}

	_wr, err := h.Write(b[cut:])
	return wr + _wr, err
}

func (h *HMACWriter) Flush() error {
	if h.buf.Len() == 0 {
		return nil
	}

	binary.LittleEndian.PutUint16(h.b, uint16(h.buf.Len()))
	_, err := h.w.Write(h.b)
	if err != nil {
		return err
	}
	_, err = h.buf.WriteTo(h.w)
	if err != nil {
		return err
	}

	_, err = h.w.Write(h.hmac.Sum(nil))

	// h.hmac.Reset()
	h.buf.Reset()

	return err
}

type HMACReader struct {
	hmac hash.Hash
	r    io.Reader

	state  byte
	amount int
	b      []byte
	hash   []byte
}

func NewHMACReader(r io.Reader, h func() hash.Hash, secret []byte) *HMACReader {
	return &HMACReader{hmac: hmac.New(h, secret), r: r, b: make([]byte, 2)}
}

func NewSHA1HMACReader(r io.Reader, secret []byte) *HMACReader {
	return NewHMACReader(r, sha1.New, secret)
}

func (h *HMACReader) Read(b []byte) (n int, err error) {
	if len(b) == 0 {
		return h.r.Read(b)
	}

	if h.state == 0 {
		if n, err := io.ReadFull(h.r, h.b); err != nil {
			if n == 0 {
				// todo, does not at all feel correct,
				// but nor is returning a network error if there is no more data
				// to be read
				return 0, io.EOF
			}
			return 0, err
		}
		h.amount = int(binary.LittleEndian.Uint16(h.b))
		h.state = 1
	}

	if h.state == 1 {
		hsize := h.hmac.Size()
		max := h.amount + hsize
		rb := b
		if len(rb) >= h.amount && len(rb) < max {
			rb = make([]byte, max)
		}
		if len(rb) > max {
			rb = rb[:max]
		}

		n, err = h.r.Read(rb)
		if err != nil {
			return
		}
		if n == 0 {
			return
		}
		h.amount -= n
		if h.amount < 0 {
			n += h.amount
			if n < 0 {
				n = 0
			}
			h.hash = append(h.hash, rb[n:]...)
			if len(h.hash) >= hsize {
				h.hash = h.hash[:hsize]
				h.state = 2
			}
		}

		n = copy(b, rb[:n])
		h.hmac.Write(rb[:n])
		// if n < len(b) {
		//return h.Read(b[n:])
		//}
	}

	if h.state == 2 {
		if !bytes.Equal(h.hmac.Sum(nil), h.hash) {
			err = errors.New("invalid mac")
			return
		}

		h.state = 0
		h.hash = h.hash[:0]
		// h.hmac.Reset()
	}

	return
}

type Decrypter struct {
	r io.Reader

	size  int
	sizeb byte

	headerRead bool

	block cipher.Block
	mode  cipher.Stream

	err error
}

func NewDecrypter(r io.Reader, secret [32]byte) *Decrypter {
	block, err := aes.NewCipher(secret[:])
	if err != nil {
		panic(err)
	}
	return &Decrypter{r: r, block: block}
}

func (d *Decrypter) readHeader() error {
	if d.headerRead {
		return nil
	}
	d.headerRead = true

	bs := d.block.BlockSize()
	iv := make([]byte, bs)
	if _, err := io.ReadFull(d.r, iv); err != nil {
		return err
	}

	d.size = bs
	d.sizeb = byte(d.size)
	d.mode = cipher.NewCFBDecrypter(d.block, iv)
	return nil
}

func (d *Decrypter) Read(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}

	if err := d.readHeader(); err != nil {
		d.err = err
		return 0, err
	}

	read, err := d.r.Read(p)
	if read != 0 {
		d.mode.XORKeyStream(p[:read], p[:read])
	}

	return read, err
}

type Encrypter struct {
	w io.Writer

	size int

	headerWritten bool

	block cipher.Block
	mode  cipher.Stream

	err error
}

func NewEncrypter(w io.Writer, secret [32]byte) *Encrypter {
	block, err := aes.NewCipher(secret[:])
	if err != nil {
		panic(err)
	}

	return &Encrypter{w: w, block: block}
}

func (d *Encrypter) writeHeader() error {
	if d.headerWritten {
		return nil
	}
	d.headerWritten = true

	bs := d.block.BlockSize()
	iv := make([]byte, bs)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return err
	}

	if _, err := d.w.Write(iv); err != nil {
		return err
	}

	d.mode = cipher.NewCFBEncrypter(d.block, iv)
	d.size = d.block.BlockSize()
	return nil
}

func (d *Encrypter) Write(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}

	if err := d.writeHeader(); err != nil {
		d.err = err
		return 0, err
	}

	buf := make([]byte, len(p))
	d.mode.XORKeyStream(buf, p)
	return d.w.Write(buf)
}

type ReadWriter struct {
	*Decrypter
	*Encrypter
}
