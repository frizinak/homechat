package channel

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/frizinak/homechat/crypto"
)

const (
	saltSize      = 255
	preMasterSize = 64
	testSize      = 32

	ServerMinKeySize = 512
	ServerKeySize    = 512

	ClientMinKeySize = 128
	ClientKeySize    = 256
)

var ErrKeyExchange = errors.New("invalid key exchange")

type (
	StringEncoder interface{ EncodeToString([]byte) string }
	StringDecoder interface{ DecodeString(string) ([]byte, error) }
)

type Hex struct{}

func (h Hex) EncodeToString(i []byte) string        { return hex.EncodeToString(i) }
func (h Hex) DecodeString(i string) ([]byte, error) { return hex.DecodeString(i) }

var (
	stringEnc StringEncoder = base64.RawURLEncoding
	stringDec StringDecoder = base64.RawURLEncoding
)

type SymmetricTestMessage struct {
	rnd []byte
	NoClose
}

func NewSymmetricTestMessage() (SymmetricTestMessage, error) {
	rnd := make([]byte, testSize)
	_, err := io.ReadFull(rand.Reader, rnd)
	return SymmetricTestMessage{rnd: rnd}, err
}

func (s SymmetricTestMessage) Equal(m Msg) bool {
	if v, ok := m.(SymmetricTestMessage); ok {
		return bytes.Equal(s.rnd, v.rnd)
	}
	return false
}

func (s SymmetricTestMessage) Binary(w BinaryWriter) error {
	w.WriteBytes(s.rnd, 8)
	return w.Err()
}

func (s SymmetricTestMessage) JSON(w io.Writer) error {
	d := map[string]string{"r": stringEnc.EncodeToString(s.rnd)}
	return json.NewEncoder(w).Encode(d)
}

func (s SymmetricTestMessage) FromBinary(r BinaryReader) (Msg, error) {
	return BinarySymmetricTestMessage(r)
}

func (s SymmetricTestMessage) FromJSON(r io.Reader) (Msg, io.Reader, error) {
	return JSONSymmetricTestMessage(r)
}

func BinarySymmetricTestMessage(r BinaryReader) (p SymmetricTestMessage, err error) {
	rnd := r.ReadBytes(8)
	if err = r.Err(); err != nil {
		err = ErrKeyExchange
		return
	}

	p.rnd = rnd

	return
}

func JSONSymmetricTestMessage(r io.Reader) (SymmetricTestMessage, io.Reader, error) {
	var s SymmetricTestMessage
	m := make(map[string]string, 1)
	nr, err := JSON(r, &m)
	if err != nil {
		return s, nr, ErrKeyExchange
	}

	rnd, err := stringDec.DecodeString(m["r"])
	if err != nil {
		return s, nr, ErrKeyExchange
	}

	s.rnd = rnd
	return s, nr, nil
}

type PubKeyServerMessage struct {
	key  *crypto.Key
	pkey *crypto.PubKey
	rnd  []byte

	NeverEqual
	NoClose
}

func NewPubKeyServerMessage(key *crypto.Key) (PubKeyServerMessage, error) {
	var p PubKeyServerMessage
	pkey, err := key.Public()
	if err != nil {
		return p, err
	}

	rnd := make([]byte, saltSize)
	if _, err = io.ReadFull(rand.Reader, rnd); err != nil {
		return p, err
	}

	p.key = key
	p.pkey = pkey
	p.rnd = rnd

	return p, nil
}

func (m PubKeyServerMessage) Fingerprint() string {
	return m.pkey.FingerprintString()
}

func (m PubKeyServerMessage) do() (der, sig []byte, err error) {
	der = m.pkey.MarshalDER()
	d := make([]byte, 0, len(der)+len(m.rnd))
	d = append(d, der...)
	d = append(d, m.rnd...)
	sig, err = m.key.Sign(d)
	return
}

func (m PubKeyServerMessage) Binary(w BinaryWriter) error {
	der, sig, err := m.do()
	if err != nil {
		return err
	}

	w.WriteBytes(der, 16)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(sig, 16)
	return w.Err()
}

func (m PubKeyServerMessage) JSON(w io.Writer) error {
	der, sig, err := m.do()
	if err != nil {
		return err
	}

	d := map[string]string{
		"k": stringEnc.EncodeToString(der),
		"r": stringEnc.EncodeToString(m.rnd),
		"s": stringEnc.EncodeToString(sig),
		// "one"
	}

	return json.NewEncoder(w).Encode(d)
}

func (m PubKeyServerMessage) FromBinary(r BinaryReader) (Msg, error) {
	return BinaryPubKeyServerMessage(r)
}

func (m PubKeyServerMessage) FromJSON(r io.Reader) (Msg, io.Reader, error) {
	return JSONPubKeyServerMessage(r)
}

func verifyServerMessage(der, rnd, sig []byte) (*crypto.PubKey, error) {
	if len(rnd) != saltSize {
		return nil, errors.New("invalid salt size")
	}

	pk := crypto.NewPubKey(ServerMinKeySize)
	if err := pk.UnmarshalDER(der); err != nil {
		return nil, fmt.Errorf("invalid publickey: %w", err)
	}
	d := make([]byte, 0, len(der)+len(rnd))
	d = append(d, der...)
	d = append(d, rnd...)
	if err := pk.Verify(d, sig); err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}
	return pk, nil
}

func BinaryPubKeyServerMessage(r BinaryReader) (p PubKeyServerMessage, err error) {
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	sig := r.ReadBytes(16)
	if err = r.Err(); err != nil {
		return
	}

	p.pkey, err = verifyServerMessage(der, rnd, sig)
	p.rnd = rnd

	return
}

func JSONPubKeyServerMessage(r io.Reader) (PubKeyServerMessage, io.Reader, error) {
	var p PubKeyServerMessage
	m := make(map[string]string, 3)
	nr, err := JSON(r, &m)
	if err != nil {
		return p, nr, err
	}

	_der, _rnd, _sig := m["k"], m["r"], m["s"] //, m["one"]
	var der, rnd, sig []byte
	if rnd, err = stringDec.DecodeString(_rnd); err != nil {
		return p, nr, fmt.Errorf("rnd not valid: %w", err)
	}
	if sig, err = stringDec.DecodeString(_sig); err != nil {
		return p, nr, fmt.Errorf("sig not valid: %w", err)
	}
	if der, err = stringDec.DecodeString(_der); err != nil {
		return p, nr, fmt.Errorf("publickey not valid: %w", err)
	}

	p.pkey, err = verifyServerMessage(der, rnd, sig)
	p.rnd = rnd

	return p, nr, err
}

type PubKeyMessage struct {
	key  *crypto.Key
	pkey *crypto.PubKey

	serverPubKey *crypto.PubKey
	rnd          []byte

	preMaster, preMasterEnc []byte

	NeverEqual
	NoClose
}

func NewPubKeyMessage(key *crypto.Key, m PubKeyServerMessage) (PubKeyMessage, error) {
	var p PubKeyMessage
	pkey, err := key.Public()
	if err != nil {
		return p, err
	}

	d := make([]byte, saltSize+preMasterSize)
	_, err = io.ReadFull(rand.Reader, d)
	if err != nil {
		return p, err
	}
	rnd, pm := d[:saltSize], d[saltSize:saltSize+preMasterSize]

	p.key = key
	p.pkey = pkey
	p.serverPubKey = m.pkey
	p.rnd = rnd
	p.preMaster = pm

	return p, nil
}

func (m PubKeyMessage) Fingerprint() string {
	return m.pkey.FingerprintString()
}

func (m PubKeyMessage) do() (der, enc, sig []byte, err error) {
	der = m.pkey.MarshalDER()
	enc, err = m.serverPubKey.Encrypt(m.preMaster)
	if err != nil {
		return
	}

	sig, err = m.key.Sign(enc)
	return
}

func (m PubKeyMessage) Binary(w BinaryWriter) error {
	der, enc, sig, err := m.do()
	if err != nil {
		return err
	}

	w.WriteBytes(der, 16)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(enc, 16)
	w.WriteBytes(sig, 16)
	return w.Err()
}

func (m PubKeyMessage) JSON(w io.Writer) error {
	der, enc, sig, err := m.do()
	if err != nil {
		return err
	}

	d := map[string]string{
		"k": stringEnc.EncodeToString(der),
		"r": stringEnc.EncodeToString(m.rnd),
		"m": stringEnc.EncodeToString(enc),
		"s": stringEnc.EncodeToString(sig),
	}

	return json.NewEncoder(w).Encode(d)
}

func (m PubKeyMessage) FromBinary(r BinaryReader) (Msg, error) {
	return BinaryPubKeyMessage(r)
}

func (m PubKeyMessage) FromJSON(r io.Reader) (Msg, io.Reader, error) {
	return JSONPubKeyMessage(r)
}

func verifyMessage(der, rnd, enc, sig []byte) (*crypto.PubKey, error) {
	if len(rnd) != saltSize {
		return nil, errors.New("invalid salt size")
	}

	pk := crypto.NewPubKey(ClientMinKeySize)
	if err := pk.UnmarshalDER(der); err != nil {
		return nil, fmt.Errorf("invalid publickey: %w", err)
	}

	if err := pk.Verify(enc, sig); err != nil {
		return nil, fmt.Errorf("invalid publickey: %w", err)
	}

	return pk, nil
}

func BinaryPubKeyMessage(r BinaryReader) (p PubKeyMessage, err error) {
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	enc := r.ReadBytes(16)
	sig := r.ReadBytes(16)
	if err = r.Err(); err != nil {
		return
	}

	p.pkey, err = verifyMessage(der, rnd, enc, sig)
	p.rnd = rnd
	p.preMasterEnc = enc

	return
}

func JSONPubKeyMessage(r io.Reader) (PubKeyMessage, io.Reader, error) {
	var p PubKeyMessage
	m := make(map[string]string, 4)
	nr, err := JSON(r, &m)
	if err != nil {
		return p, nr, err
	}

	_der, _rnd, _enc, _sig := m["k"], m["r"], m["m"], m["s"]
	var der, rnd, enc, sig []byte
	if der, err = stringDec.DecodeString(_der); err != nil {
		return p, nr, fmt.Errorf("publickey not valid: %w", err)
	}
	if rnd, err = stringDec.DecodeString(_rnd); err != nil {
		return p, nr, fmt.Errorf("rnd not valid: %w", err)
	}
	if sig, err = stringDec.DecodeString(_sig); err != nil {
		return p, nr, fmt.Errorf("sig not valid: %w", err)
	}
	if enc, err = stringDec.DecodeString(_enc); err != nil {
		return p, nr, fmt.Errorf("premaster not valid: %w", err)
	}

	p.pkey, err = verifyMessage(der, rnd, enc, sig)
	p.rnd = rnd
	p.preMasterEnc = enc

	return p, nr, err
}

type (
	DeriveSecret   func(label CryptoLabel) []byte
	DeriveSecret32 func(label CryptoLabel) [32]byte
)

type CryptoLabel string

const (
	CryptoClientWrite    CryptoLabel = "write"
	CryptoClientRead     CryptoLabel = "read"
	CryptoClientMacWrite CryptoLabel = "mac-write"
	CryptoClientMacRead  CryptoLabel = "mac-read"

	CryptoServerWrite CryptoLabel = CryptoClientRead
	CryptoServerRead  CryptoLabel = CryptoClientWrite

	CryptoServerMacWrite CryptoLabel = CryptoClientMacRead
	CryptoServerMacRead  CryptoLabel = CryptoClientMacWrite
)

func CommonSecret(c PubKeyMessage, s PubKeyServerMessage, serverPrivate *crypto.Key, size int) (DeriveSecret, error) {
	clientRandom := c.rnd
	serverRandom := s.rnd
	preMaster := c.preMaster
	if preMaster == nil {
		var err error
		preMaster, err = serverPrivate.Decrypt(c.preMasterEnc)
		if err != nil {
			return nil, err
		}
	}

	if len(preMaster) != preMasterSize {
		return nil, errors.New("invalid data to generate common secret")
	}

	return func(label CryptoLabel) []byte {
		seed := make([]byte, 0, len(label)+len(clientRandom)+len(serverRandom))
		seed = append(seed, label...)
		seed = append(seed, clientRandom...)
		seed = append(seed, serverRandom...)
		master := make([]byte, size)
		crypto.HMAC(master, preMaster, seed)

		return master
	}, nil
}

func CommonSecret32(c PubKeyMessage, s PubKeyServerMessage, serverPrivate *crypto.Key) (DeriveSecret32, error) {
	d, err := CommonSecret(c, s, serverPrivate, 32)
	if err != nil {
		return nil, err
	}

	return func(label CryptoLabel) [32]byte {
		n := d(label)
		if len(n) != 32 {
			panic("invalid length")
		}
		var b [32]byte
		if 32 != copy(b[:], n) {
			panic("copy failed")
		}

		return b
	}, nil
}
