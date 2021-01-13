package crypto

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"time"
)

var (
	ErrKeyExists = errors.New("a key already exists")
	ErrNotRSA    = errors.New("key is not an rsa key")
)

const (
	typePrivate = "RSA PRIVATE KEY"
	typePublic  = "PUBLIC KEY"
)

const size = 4096

func HMAC(result, secret, seed []byte) {
	// copy from stdlib crypto/tls/prf
	hash := sha512.New384
	h := hmac.New(hash, secret)
	h.Write(seed)
	a := h.Sum(nil)

	j := 0
	for j < len(result) {
		h.Reset()
		h.Write(a)
		h.Write(seed)
		b := h.Sum(nil)
		copy(result[j:], b)
		j += len(b)

		h.Reset()
		h.Write(a)
		a = h.Sum(nil)
	}
}

func hasher() hash.Hash {
	return sha256.New()
}

func unmarshalPEM(typeHeader string, data []byte) ([]byte, error) {
	block, _ := pem.Decode(data)
	if block.Type != typeHeader {
		return nil, ErrNotRSA
	}

	return block.Bytes, nil
}

func marshalPEM(typeHeader string, data []byte) ([]byte, error) {
	block := &pem.Block{Type: typeHeader, Bytes: data}
	w := bytes.NewBuffer(nil)
	err := pem.Encode(w, block)
	return w.Bytes(), err
}

func EnsureKey(file string) (*Key, error) {
	k, err := KeyFromFile(file)
	if !os.IsNotExist(err) {
		return k, err
	}

	k = NewKey()
	if err := k.Generate(); err != nil {
		return nil, err
	}

	d, err := k.MarshalPEM()
	if err != nil {
		return nil, err
	}

	tmp := fmt.Sprintf("%s.%d.tmp", file, time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o400)
	if err != nil {
		return nil, err
	}

	_, err = f.Write(d)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return k, os.Rename(tmp, file)
}

func KeyFromFile(file string) (*Key, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	k, err := KeyFromReader(f)
	f.Close()
	return k, err
}

func KeyFromReader(r io.Reader) (*Key, error) {
	d, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	key := NewKey()
	return key, key.UnmarshalPEM(d)
}

type Key struct {
	private *rsa.PrivateKey
}

func NewKey() *Key {
	return &Key{}
}

func (k *Key) Public() (*PubKey, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}

	return &PubKey{k.private.Public().(*rsa.PublicKey)}, nil
}

func (k *Key) Decrypt(data []byte) ([]byte, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}
	return rsa.DecryptOAEP(hasher(), rand.Reader, k.private, data, nil)
}

func (k *Key) Generate() error {
	if k.private != nil {
		return ErrKeyExists
	}

	priv, err := rsa.GenerateKey(rand.Reader, size)
	if err != nil {
		return err
	}

	k.private = priv
	return nil
}

func (k *Key) UnmarshalPEM(data []byte) error {
	der, err := unmarshalPEM(typePrivate, data)
	if err != nil {
		return err
	}

	return k.UnmarshalDER(der)
}

func (k *Key) MarshalPEM() ([]byte, error) {
	data, err := k.MarshalDER()
	if err != nil {
		return nil, err
	}
	return marshalPEM(typePrivate, data)
}

func (k *Key) MarshalDER() ([]byte, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}

	return x509.MarshalPKCS8PrivateKey(k.private)
}

func (k *Key) UnmarshalDER(data []byte) error {
	key, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return err
	}

	rsa, ok := key.(*rsa.PrivateKey)
	if !ok {
		return ErrNotRSA
	}

	k.private = rsa
	return nil
}

type PubKey struct {
	public *rsa.PublicKey
}

func NewPubKey() *PubKey {
	return &PubKey{}
}

func (k *PubKey) Encrypt(data []byte) ([]byte, error) {
	return rsa.EncryptOAEP(hasher(), rand.Reader, k.public, data, nil)
}

func (k *PubKey) MaxPayload() int {
	h := hasher()
	return k.public.Size() - 2*h.Size() - 2
}

func (k *PubKey) UnmarshalPEM(data []byte) error {
	der, err := unmarshalPEM(typePublic, data)
	if err != nil {
		return err
	}

	return k.UnmarshalDER(der)
}

func (k *PubKey) MarshalPEM() ([]byte, error) {
	return marshalPEM(typePublic, k.MarshalDER())
}

func (k *PubKey) MarshalDER() []byte {
	return x509.MarshalPKCS1PublicKey(k.public)
}

func (k *PubKey) UnmarshalDER(data []byte) error {
	key, err := x509.ParsePKCS1PublicKey(data)
	if err != nil {
		return err
	}

	k.public = key
	return nil
}
