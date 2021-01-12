package crypto

import (
	"bufio"
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

func Encrypt(
	r io.Reader,
	w io.Writer,
	passphrase []byte,
	saltSize uint16,
	cost uint8,
) error {
	salt, err := salt(saltSize)
	if err != nil {
		return err
	}

	key, err := Key(passphrase, salt, cost)
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

	if err := binary.Write(w, binary.LittleEndian, cost); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, saltSize); err != nil {
		return err
	}

	if _, err := w.Write(salt); err != nil {
		return err
	}

	if _, err := w.Write(iv); err != nil {
		return err
	}

	return encrypt(
		r,
		w,
		block,
		iv,
	)
}

func Header(r io.Reader) (salt, iv []byte, cost uint8, err error) {
	var saltSize uint16
	if err = binary.Read(r, binary.LittleEndian, &cost); err != nil {
		return
	}

	if err = binary.Read(r, binary.LittleEndian, &saltSize); err != nil {
		return
	}

	header := make([]byte, saltSize+aes.BlockSize)
	if _, err = io.ReadFull(r, header); err != nil {
		return
	}

	salt = header[:saltSize]
	iv = header[saltSize:]

	return
}

func DecryptWithHeader(
	r io.Reader,
	w io.Writer,
	passphrase []byte,
	salt,
	iv []byte,
	cost uint8,
) error {
	key, err := Key(passphrase, salt, cost)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	return decrypt(
		r,
		w,
		block,
		iv,
	)
}

func Decrypt(
	r io.Reader,
	w io.Writer,
	passphrase []byte,
) error {
	salt, iv, cost, err := Header(r)
	if err != nil {
		return err
	}

	return DecryptWithHeader(r, w, passphrase, salt, iv, cost)
}

func encrypt(
	r io.Reader,
	w io.Writer,
	block cipher.Block,
	iv []byte,
) error {
	size := block.BlockSize()
	mode := cipher.NewCBCEncrypter(block, iv)

	out := make([]byte, size)
	buf := make([]byte, size)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			for i := n; i < size; i++ {
				buf[i] = byte(size - n)
			}
			mode.CryptBlocks(out, buf)
			if _, err := w.Write(out); err != nil {
				return err
			}
		}

		if err != nil {
			return err
		}
	}
}

func decrypt(
	r io.Reader,
	w io.Writer,
	block cipher.Block,
	iv []byte,
) error {
	size := block.BlockSize()
	sizeb := byte(size)
	mode := cipher.NewCBCDecrypter(block, iv)
	rb := bufio.NewReader(r)

	buf := make([]byte, size)
	out := make([]byte, size)
	for {
		padded := sizeb
		_, err := io.ReadFull(rb, buf)
		if err != nil {
			return err
		}

		mode.CryptBlocks(out, buf)
		l := out[size-1]
		if l < sizeb {
			padded = sizeb - l
			for i := size - int(l); i < size; i++ {
				if out[i] != l {
					padded = sizeb
					break
				}
			}
		}

		if _, err := w.Write(out[:padded]); err != nil {
			return err
		}
	}
}

func Key(passphrase, salt []byte, cost uint8) ([]byte, error) {
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
	r, pr io.Reader
	err   error
}

func NewDecrypter(r io.Reader, pass []byte) io.Reader {
	pr, pw := io.Pipe()
	d := &Decrypter{r: r, pr: pr}
	go func() {
		d.err = Decrypt(r, pw, pass)
		d.gerr(pw.Close())
	}()
	return d
}

func (d *Decrypter) Read(p []byte) (int, error) {
	n, err := d.pr.Read(p)
	d.gerr(err)
	return n, d.err
}

func (d *Decrypter) gerr(err error) {
	if d.err == nil && err != nil {
		d.err = err
	}
}

type Encrypter struct {
	w, pw io.Writer
	err   error
}

func NewEncrypter(
	w io.Writer,
	pass []byte,
	saltSize uint16,
	cost uint8,
) io.Writer {
	pr, pw := io.Pipe()
	d := &Encrypter{w: w, pw: pw}
	go func() {
		d.err = Encrypt(pr, w, pass, saltSize, cost)
		d.gerr(pw.Close())
	}()
	return d
}

func (d *Encrypter) Write(p []byte) (int, error) {
	n, err := d.pw.Write(p)
	d.gerr(err)
	return n, d.err
}

func (d *Encrypter) gerr(err error) {
	if d.err == nil && err != nil {
		d.err = err
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
