package channel

import (
	"crypto/rand"
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/crypto"
)

const (
	saltSize      = 64
	preMasterSize = 64
	masterSize    = 64

	ServerMinKeySize = 512
	ServerKeySize    = 512

	ClientMinKeySize = 128
	ClientKeySize    = 256
)

type PubKeyServerMessage struct {
	key  *crypto.Key
	pkey *crypto.PubKey
	rnd  []byte

	NeverEqual
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

func (m PubKeyServerMessage) Binary(w *binary.Writer) error {
	der := m.pkey.MarshalDER()
	d := make([]byte, 0, len(der)+len(m.rnd))
	d = append(d, der...)
	d = append(d, m.rnd...)
	sig, err := m.key.Sign(d)
	if err != nil {
		return err
	}

	w.WriteBytes(der, 16)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(sig, 16)
	return w.Err()
}

func (m PubKeyServerMessage) JSON(w io.Writer) error {
	return errors.New("not impl yet todo")
}

func (m PubKeyServerMessage) FromBinary(r *binary.Reader) (Msg, error) {
	return BinaryPubKeyServerMessage(r)
}

func (m PubKeyServerMessage) FromJSON(r io.Reader) (Msg, io.Reader, error) {
	return JSONPubKeyServerMessage(r)
}

func BinaryPubKeyServerMessage(r *binary.Reader) (PubKeyServerMessage, error) {
	var p PubKeyServerMessage
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	sig := r.ReadBytes(16)
	if err := r.Err(); err != nil {
		return p, err
	}

	d := make([]byte, 0, len(der)+len(rnd))
	d = append(d, der...)
	d = append(d, rnd...)

	pk := crypto.NewPubKey(ServerMinKeySize)
	if err := pk.UnmarshalDER(der); err != nil {
		return p, err
	}

	if err := pk.Verify(d, sig); err != nil {
		return p, err
	}

	p.pkey = pk
	p.rnd = rnd

	return p, nil
}

func JSONPubKeyServerMessage(r io.Reader) (PubKeyServerMessage, io.Reader, error) {
	return PubKeyServerMessage{}, r, errors.New("not impl yet todo")
	// m := make(map[string]string)
	// nr, err := JSON(r, &m)
	// return nil, nr, err
}

type PubKeyMessage struct {
	key  *crypto.Key
	pkey *crypto.PubKey

	serverPubKey *crypto.PubKey
	rnd          []byte

	preMaster, preMasterEnc []byte

	NeverEqual
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

func (m PubKeyMessage) Binary(w *binary.Writer) error {
	enc, err := m.serverPubKey.Encrypt(m.preMaster)
	if err != nil {
		return err
	}

	sig, err := m.key.Sign(enc)
	if err != nil {
		return err
	}

	w.WriteBytes(m.pkey.MarshalDER(), 16)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(enc, 16)
	w.WriteBytes(sig, 16)
	return w.Err()
}

func (m PubKeyMessage) JSON(w io.Writer) error {
	return errors.New("not impl yet todo")
}

func (m PubKeyMessage) FromBinary(r *binary.Reader) (Msg, error) {
	return BinaryPubKeyMessage(r)
}

func (m PubKeyMessage) FromJSON(r io.Reader) (Msg, io.Reader, error) {
	return JSONPubKeyMessage(r)
}

func BinaryPubKeyMessage(r *binary.Reader) (PubKeyMessage, error) {
	var p PubKeyMessage
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	enc := r.ReadBytes(16)
	sig := r.ReadBytes(16)
	if err := r.Err(); err != nil {
		return p, err
	}

	pk := crypto.NewPubKey(ClientMinKeySize)
	if err := pk.UnmarshalDER(der); err != nil {
		return p, err
	}

	if err := pk.Verify(enc, sig); err != nil {
		return p, err
	}

	p.pkey = pk
	p.rnd = rnd
	p.preMasterEnc = enc
	return p, nil
}

func JSONPubKeyMessage(r io.Reader) (PubKeyMessage, io.Reader, error) {
	return PubKeyMessage{}, r, errors.New("not impl yet todo")
	// m := make(map[string]string)
	// nr, err := JSON(r, &m)
	// return nil, nr, err
}

type DeriveSecret func(label CryptoLabel) []byte

type CryptoLabel string

const (
	CryptoClientWrite CryptoLabel = "write"
	CryptoClientRead  CryptoLabel = "read"

	CryptoServerWrite CryptoLabel = CryptoClientRead
	CryptoServerRead  CryptoLabel = CryptoClientWrite
)

func CommonSecret(c PubKeyMessage, s PubKeyServerMessage, serverPrivate *crypto.Key) (DeriveSecret, error) {
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
		master := make([]byte, masterSize)
		crypto.HMAC(master, preMaster, seed)

		return master
	}, nil
}
