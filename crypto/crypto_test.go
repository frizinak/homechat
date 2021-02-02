package crypto

import (
	"bytes"
	"crypto/rsa"
	"io"
	"io/ioutil"
	"math/rand"
	"testing"
)

func rnd(size int) []byte {
	input := make([]byte, size)
	n, err := rand.Read(input)
	if err != nil {
		panic(err)
	}
	if n != size {
		panic("short read")
	}
	return input
}

func TestKeys(t *testing.T) {
	k := NewKey(256, 256)
	if err := k.Generate(); err != nil {
		t.Error(err)
	}

	cur := rsa.PrivateKey{}
	cur = *k.private
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		t.Fatal(err)
	}
	if !k.private.Equal(&cur) {
		t.Fatal("key was regenerated")
	}

	loadkey := NewKey(256, 256)
	d, err := k.MarshalPEM()
	if err != nil {
		t.Fatal(err)
	}

	loadkey.UnmarshalPEM(d)
	if !k.private.Equal(loadkey.private) {
		t.Fatal(err)
	}

	pk, err := k.Public()
	if err != nil {
		t.Fatal(err)
	}

	lpk, err := loadkey.Public()
	if err != nil {
		t.Fatal(err)
	}

	if !pk.public.Equal(lpk.public) {
		t.Fatal("pub key does not match")
	}

	input := rnd(pk.MaxPayload() + 1)

	if _, err := pk.Encrypt(input); err == nil {
		t.Fatal("max payload calc is off")
	}

	input = rnd(pk.MaxPayload())
	enc, err := pk.Encrypt(input)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := k.Decrypt(enc)
	if !bytes.Equal(input, dec) {
		t.Fatal("neq")
	}
}

func TestEncDec(t *testing.T) {
	const n = 32
	_r := rnd(n * 2)
	var wkey, rkey [n]byte
	for i := 0; i < n; i++ {
		wkey[i] = _r[i]
		rkey[i] = _r[i+n]
	}

	input := rnd(4096)
	one := bytes.NewReader(input)
	three := bytes.NewBuffer(nil)
	two := NewEncrypter(three, rkey)
	if _, err := io.Copy(two, one); err != nil {
		t.Fatal(err)
	}

	five := bytes.NewBuffer(nil)
	four := ReadWriter{
		NewDecrypter(three, rkey),
		NewEncrypter(five, wkey),
	}

	output, err := ioutil.ReadAll(four)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(output, input) {
		t.Fatal("input != output when reading from encdec")
	}

	four.Write(input)
	six := NewDecrypter(five, wkey)
	output, err = ioutil.ReadAll(six)
	if !bytes.Equal(output, input) {
		t.Fatal("input != output when reading from decrypter")
	}
}

func TestHMAC(t *testing.T) {
	sec := rnd(32)
	buf := bytes.NewBuffer(nil)
	w := NewSHA1HMACWriter(buf, sec, 100)

	write := rnd(1 << 16)
	w.Write(write[0:10])
	if err := w.Flush(); err != nil {
		t.Error(err)
	}

	w.Write(write[10:300])
	w.Write(write[300:600])
	w.Write(write[600:])
	if err := w.Flush(); err != nil {
		t.Error(err)
	}

	r := NewSHA1HMACReader(buf, sec)
	read, err := ioutil.ReadAll(r)
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(write, read) {
		t.Errorf("data does not match (len: %d %d)", len(write), len(read))
	}
}
