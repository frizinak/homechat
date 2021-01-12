package crypto

import (
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

type DecrypterConfig struct {
	MinSaltSize uint16
	MinCost     uint8
}

type EncrypterConfig struct {
	SaltSize uint16
	Cost     uint8
}

const random = 20

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

type Decrypter struct {
	r io.Reader

	minSaltSize uint16
	minCost     uint8

	pass  []byte
	size  int
	sizeb byte

	headerRead bool

	mode cipher.Stream
}

func NewDecrypter(c DecrypterConfig, r io.Reader, pass []byte) io.Reader {
	d := &Decrypter{r: r, pass: pass, minSaltSize: c.MinSaltSize, minCost: c.MinCost}
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

	if saltSize < d.minSaltSize || cost < d.minCost {
		return errors.New("data does not conform to requirements")
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
	d.mode = cipher.NewCFBDecrypter(block, iv)

	if cap(header) < random {
		header = make([]byte, random)
	}
	_, err = io.ReadFull(d, header[:random])
	return err
}

func (d *Decrypter) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := d.readHeader(); err != nil {
		return 0, err
	}

	read, err := d.r.Read(p)
	if read == 0 {
		return 0, err
	}

	d.mode.XORKeyStream(p[:read], p[:read])
	return read, nil
}

type Encrypter struct {
	w io.Writer

	pass          []byte
	saltSize      uint16
	cost          uint8
	headerWritten bool

	mode cipher.Stream
	size int
}

func NewEncrypter(
	c EncrypterConfig,
	w io.Writer,
	pass []byte,
) io.Writer {
	d := &Encrypter{
		w:        w,
		pass:     pass,
		saltSize: c.SaltSize,
		cost:     c.Cost,
	}
	return d
}

func (d *Encrypter) writeHeader() error {
	if d.headerWritten {
		return nil
	}
	d.headerWritten = true

	randsaltiv := make([]byte, random+d.saltSize+aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, randsaltiv); err != nil {
		return err
	}
	rand, saltiv := randsaltiv[:random], randsaltiv[random:]
	salt, iv := saltiv[:d.saltSize], saltiv[d.saltSize:]

	key, err := SKey(d.pass, salt, d.cost)
	if err != nil {
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

	if _, err := d.w.Write(saltiv); err != nil {
		return err
	}

	d.mode = cipher.NewCFBEncrypter(block, iv)
	d.size = block.BlockSize()

	_, err = d.Write(rand)
	return err
}

func (d *Encrypter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := d.writeHeader(); err != nil {
		return 0, err
	}

	buf := make([]byte, len(p))
	d.mode.XORKeyStream(buf, p)
	return d.w.Write(buf)
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
	ec EncrypterConfig,
	dc DecrypterConfig,
) io.ReadWriter {
	return &EncDec{
		NewDecrypter(dc, r, readPass),
		NewEncrypter(ec, w, writePass),
	}
}
