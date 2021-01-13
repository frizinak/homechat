package crypto

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
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
	ErrKeyExists   = errors.New("a key already exists")
	ErrKeyTooSmall = errors.New("key is too small")
	ErrNotRSA      = errors.New("key is not an rsa key")
)

const (
	typePrivate = "RSA PRIVATE KEY"
	typePublic  = "PUBLIC KEY"
)

const pkgMinBytes = 128

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

func sigHasher() (crypto.Hash, hash.Hash) {
	return crypto.SHA256, sha256.New()
}

func unmarshalPEM(typeHeader string, data []byte) ([]byte, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("data is not pem encoded")
	}
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

func EnsureKey(file string, minBytes, desiredBytes int) (*Key, error) {
	k, err := KeyFromFile(file, minBytes, desiredBytes)
	if !os.IsNotExist(err) {
		return k, err
	}

	k = NewKey(minBytes, desiredBytes)
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

func KeyFromFile(file string, minBytes, desiredBytes int) (*Key, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	k, err := KeyFromReader(f, minBytes, desiredBytes)
	f.Close()
	return k, err
}

func KeyFromReader(r io.Reader, minBytes, desiredBytes int) (*Key, error) {
	d, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	key := NewKey(minBytes, desiredBytes)
	return key, key.UnmarshalPEM(d)
}

type Key struct {
	private *rsa.PrivateKey
	min     int
	size    int
}

func NewKey(minBytes, desiredBytes int) *Key {
	if minBytes < pkgMinBytes {
		minBytes = pkgMinBytes
	}
	if desiredBytes < minBytes {
		desiredBytes = minBytes
	}
	return &Key{min: minBytes, size: desiredBytes}
}

func (k *Key) Size() int { return k.private.Size() }

func (k *Key) Public() (*PubKey, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}

	return &PubKey{k.private.Public().(*rsa.PublicKey), k.min}, nil
}

func (k *Key) Decrypt(data []byte) ([]byte, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}
	return rsa.DecryptOAEP(hasher(), rand.Reader, k.private, data, nil)
}

func (k *Key) Sign(data []byte) ([]byte, error) {
	if err := k.Generate(); err != nil && err != ErrKeyExists {
		return nil, err
	}
	t, h := sigHasher()
	if _, err := h.Write(data); err != nil {
		return nil, err
	}

	return rsa.SignPKCS1v15(rand.Reader, k.private, t, h.Sum(nil))
}

func (k *Key) Generate() error {
	if k.private != nil {
		return ErrKeyExists
	}

	priv, err := rsa.GenerateKey(rand.Reader, k.size*8)
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

	if rsa.Size() < k.min {
		return ErrKeyTooSmall
	}

	k.private = rsa
	return nil
}

type PubKey struct {
	public *rsa.PublicKey
	min    int
}

func NewPubKey(minBytes int) *PubKey {
	if minBytes < pkgMinBytes {
		minBytes = pkgMinBytes
	}
	return &PubKey{min: minBytes}
}

func (k *PubKey) Size() int { return k.public.Size() }

func (k *PubKey) Encrypt(data []byte) ([]byte, error) {
	return rsa.EncryptOAEP(hasher(), rand.Reader, k.public, data, nil)
}

func (k *PubKey) Verify(data, sig []byte) error {
	t, h := sigHasher()
	if _, err := h.Write(data); err != nil {
		return err
	}

	return rsa.VerifyPKCS1v15(k.public, t, h.Sum(nil), sig)
}

func (k *PubKey) Fingerprint() [32]byte {
	b := k.public.N.Bytes()
	r := make([]byte, 0, 1+len(b)+1+8)
	var sign byte
	if k.public.N.Sign() == -1 {
		sign = 1
	}
	r = append(r, sign)
	r = append(r, b...)
	sign = 0
	if k.public.E < 0 {
		sign = 1
	}
	r = append(r, sign)
	r = r[:cap(r)]
	binary.LittleEndian.PutUint64(r[len(r)-8:], uint64(k.public.E))
	return sha256.Sum256(r)
}

func (k *PubKey) FingerprintString() string {
	n := k.Fingerprint()
	dst := make([]byte, hex.EncodedLen(len(n))+len(n)-1)
	var off int
	for i := 0; i < len(n); i++ {
		off = hex.EncodedLen(i) + i
		w := hex.Encode(dst[off:], n[i:i+1])
		if i != len(n)-1 {
			dst[off+w] = ':'
		}
	}
	return string(dst)
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

	if key.Size() < k.min {
		return ErrKeyTooSmall
	}

	k.public = key
	return nil
}
