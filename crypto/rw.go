package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
)

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
	if len(p) == 0 {
		return 0, nil
	}
	if d.err != nil {
		return 0, d.err
	}

	if err := d.readHeader(); err != nil {
		d.err = err
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
	if len(p) == 0 {
		return 0, nil
	}
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
