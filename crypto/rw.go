package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/scrypt"
)

const (
	MaxCost     = 30
	MinCost     = 6
	MinSaltSize = 8
)

func SKey(passphrase, salt []byte, cost uint8) ([]byte, error) {
	if cost > MaxCost {
		return nil, errors.New("scrypt cost too high")
	} else if cost < MinCost {
		return nil, errors.New("scrypt cost too low")
	}

	if len(salt) < MinSaltSize {
		return nil, errors.New("Salt too short")
	}

	return scrypt.Key(passphrase, salt, 1<<cost, 8, 1, 32)
}

func salt(size uint16) ([]byte, error) {
	salt := make([]byte, size)
	_, err := io.ReadFull(rand.Reader, salt)
	return salt, err
}

type Decrypter struct {
	r   io.Reader
	buf *bytes.Buffer

	pass  []byte
	size  int
	sizeb byte

	headerRead bool
	eof        bool

	mode cipher.BlockMode
}

func NewDecrypter(r io.Reader, pass []byte) io.Reader {
	d := &Decrypter{r: r, pass: pass, buf: bytes.NewBuffer(make([]byte, 0, aes.BlockSize))}
	return d
}

func (d *Decrypter) readHeader() error {
	if d.headerRead {
		return nil
	}

	d.headerRead = true

	var saltSize uint16
	var cost uint8
	if err := binary.Read(d.r, binary.LittleEndian, &cost); err != nil {
		return err
	}

	if err := binary.Read(d.r, binary.LittleEndian, &saltSize); err != nil {
		return err
	}

	header := make([]byte, saltSize+aes.BlockSize)
	if _, err := io.ReadFull(d.r, header); err != nil {
		return err
	}

	salt := header[:saltSize]
	iv := header[saltSize:]

	key, err := SKey(d.pass, salt, cost)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	d.size = block.BlockSize()
	d.sizeb = byte(d.size)
	d.mode = cipher.NewCBCDecrypter(block, iv)

	return nil
}

func (d *Decrypter) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := d.readHeader(); err != nil {
		return 0, err
	}

	if d.buf.Len() != 0 {
		n, err := d.buf.Read(p)
		if err == io.EOF {
			err = nil
		}
		return n, err
	}

	buf := make([]byte, d.size)
	out := make([]byte, d.size)
	padded := d.sizeb
	read, err := io.ReadFull(d.r, buf)
	if read == 0 {
		return 0, err
	}

	d.mode.CryptBlocks(out, buf)
	l := out[d.size-1]
	if l < d.sizeb {
		padded = d.sizeb - l
		for i := d.sizeb - l; i < d.sizeb; i++ {
			if out[i] != l {
				padded = d.sizeb
				break
			}
		}
	}

	d.buf.Write(out[:padded])
	return d.buf.Read(p)
}

type Encrypter struct {
	w io.Writer

	pass          []byte
	saltSize      uint16
	cost          uint8
	headerWritten bool

	mode cipher.BlockMode
	size int
}

func NewEncrypter(
	w io.Writer,
	pass []byte,
	saltSize uint16,
	cost uint8,
) io.Writer {
	d := &Encrypter{
		w:        w,
		pass:     pass,
		saltSize: saltSize,
		cost:     cost,
	}
	return d
}

func (d *Encrypter) writeHeader() error {
	if d.headerWritten {
		return nil
	}

	d.headerWritten = true
	salt, err := salt(d.saltSize)
	if err != nil {
		return err
	}

	key, err := SKey(d.pass, salt, d.cost)
	if err != nil {
		return err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	if err := binary.Write(d.w, binary.LittleEndian, d.cost); err != nil {
		return err
	}

	if err := binary.Write(d.w, binary.LittleEndian, d.saltSize); err != nil {
		return err
	}

	if _, err := d.w.Write(salt); err != nil {
		return err
	}

	if _, err := d.w.Write(iv); err != nil {
		return err
	}

	d.mode = cipher.NewCBCEncrypter(block, iv)
	d.size = block.BlockSize()

	return nil
}

func (d *Encrypter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := d.writeHeader(); err != nil {
		return 0, err
	}

	r := bytes.NewReader(p)
	written := 0
	out := make([]byte, d.size)
	buf := make([]byte, d.size)
	for {
		n, err := r.Read(buf[:d.size-1])
		if n > 0 {
			for i := n; i < d.size; i++ {
				buf[i] = byte(d.size - n)
			}
			d.mode.CryptBlocks(out, buf)
			_n, err := d.w.Write(out)
			written += _n
			if err != nil {
				return written, err
			}
		}

		if n == 0 && err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

type EncDec struct {
	io.Reader
	io.Writer
}

func NewEncDec(
	r io.Reader,
	w io.Writer,
	readPass []byte,
	writePass []byte,
	saltSize uint16,
	cost uint8,
) io.ReadWriter {
	return &EncDec{
		NewDecrypter(r, readPass),
		NewEncrypter(w, writePass, saltSize, cost),
	}
}
