package channel

import (
	"crypto/rand"
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/crypto"
)

const saltSize = 32
const preMasterSize = 48
const masterSize = 48

type PubKeyMessage struct {
	key *crypto.PubKey
	rnd []byte

	NeverEqual
}

func NewPubKeyMessage(pubkey *crypto.PubKey) (PubKeyMessage, error) {
	var p PubKeyMessage
	rnd := make([]byte, saltSize)
	_, err := io.ReadFull(rand.Reader, rnd)
	if err != nil {
		return p, err
	}

	p.key = pubkey
	p.rnd = rnd

	return p, nil
}

func (m PubKeyMessage) Binary(w *binary.Writer) error {
	w.WriteBytes(m.key.MarshalDER(), 32)
	w.WriteBytes(m.rnd, 8)
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
	der := r.ReadBytes(32)
	rnd := r.ReadBytes(8)
	if err := r.Err(); err != nil {
		return p, err
	}

	pk := crypto.NewPubKey()
	if err := pk.UnmarshalDER(der); err != nil {
		return p, err
	}

	p.key = pk
	p.rnd = rnd

	return p, nil
}

func JSONPubKeyMessage(r io.Reader) (PubKeyMessage, io.Reader, error) {
	return PubKeyMessage{}, r, errors.New("not impl yet todo")
	// m := make(map[string]string)
	// nr, err := JSON(r, &m)
	// return nil, nr, err
}

type PubKeyServerMessage struct {
	key          *crypto.Key
	pkey         *crypto.PubKey
	client       PubKeyMessage
	rnd          []byte
	preMaster    []byte
	preMasterEnc []byte

	NeverEqual
}

func NewPubKeyServerMessage(key *crypto.Key, m PubKeyMessage) (PubKeyServerMessage, error) {
	var p PubKeyServerMessage
	pkey, err := key.Public()
	if err != nil {
		return p, err
	}

	rndpm := make([]byte, saltSize+preMasterSize)
	_, err = io.ReadFull(rand.Reader, rndpm)
	if err != nil {
		return p, err
	}
	rnd, pm := rndpm[:saltSize], rndpm[saltSize:]

	p.key = key
	p.pkey = pkey
	p.client = m
	p.rnd = rnd
	p.preMaster = pm

	return p, nil
}

func (m PubKeyServerMessage) Binary(w *binary.Writer) error {
	enc, err := m.client.key.Encrypt(m.preMaster)
	if err != nil {
		return err
	}

	w.WriteBytes(m.pkey.MarshalDER(), 32)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(enc, 32)
	// todo sign
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
	der := r.ReadBytes(32)
	rnd := r.ReadBytes(8)
	enc := r.ReadBytes(32)
	if err := r.Err(); err != nil {
		return p, err
	}

	pk := crypto.NewPubKey()
	if err := pk.UnmarshalDER(der); err != nil {
		return p, err
	}

	p.pkey = pk
	p.rnd = rnd
	p.preMasterEnc = enc
	return p, nil
}

func JSONPubKeyServerMessage(r io.Reader) (PubKeyServerMessage, io.Reader, error) {
	return PubKeyServerMessage{}, r, errors.New("not impl yet todo")
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

func CommonSecret(c PubKeyMessage, s PubKeyServerMessage, clientPrivate *crypto.Key) (DeriveSecret, error) {
	clientRandom := c.rnd
	serverRandom := s.rnd
	preMaster := s.preMaster
	if preMaster == nil {
		var err error
		preMaster, err = clientPrivate.Decrypt(s.preMasterEnc)
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
