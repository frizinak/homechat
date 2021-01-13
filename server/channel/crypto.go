package channel

import (
	"crypto/rand"
	"errors"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/crypto"
)

const (
	saltSize      = 32
	preMasterSize = 48
	masterSize    = 48
	signSize      = 32

	AsymmetricMinKeySize = 256
	AsymmetricKeySize    = 512
)

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
	w.WriteBytes(m.key.MarshalDER(), 16)
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
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	if err := r.Err(); err != nil {
		return p, err
	}

	pk := crypto.NewPubKey(AsymmetricMinKeySize)
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
	rnd       []byte
	forServer struct {
		key       *crypto.Key
		client    PubKeyMessage
		preMaster []byte
	}
	forClient struct {
		pkey         *crypto.PubKey
		preMasterEnc []byte
		sign         []byte
	}

	NeverEqual
}

func NewPubKeyServerMessage(key *crypto.Key, m PubKeyMessage) (PubKeyServerMessage, error) {
	var p PubKeyServerMessage
	pkey, err := key.Public()
	if err != nil {
		return p, err
	}

	d := make([]byte, saltSize+preMasterSize+signSize)
	_, err = io.ReadFull(rand.Reader, d)
	if err != nil {
		return p, err
	}
	rnd, pm, sign := d[:saltSize], d[saltSize:saltSize+preMasterSize], d[saltSize+preMasterSize:]

	p.forServer.key = key
	p.forClient.pkey = pkey
	p.forServer.client = m
	p.rnd = rnd
	p.forServer.preMaster = pm
	p.forClient.sign = sign

	return p, nil
}

func (m PubKeyServerMessage) Binary(w *binary.Writer) error {
	enc, err := m.forServer.client.key.Encrypt(m.forServer.preMaster)
	if err != nil {
		return err
	}

	sig, err := m.forServer.key.Sign(m.forClient.sign)
	if err != nil {
		return err
	}

	w.WriteBytes(m.forClient.pkey.MarshalDER(), 16)
	w.WriteBytes(m.rnd, 8)
	w.WriteBytes(enc, 16)
	w.WriteBytes(m.forClient.sign, 16)
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

func (m PubKeyServerMessage) Fingerprint() string {
	return m.forClient.pkey.FingerprintString()
}

func BinaryPubKeyServerMessage(r *binary.Reader) (PubKeyServerMessage, error) {
	var p PubKeyServerMessage
	der := r.ReadBytes(16)
	rnd := r.ReadBytes(8)
	enc := r.ReadBytes(16)
	sign := r.ReadBytes(16)
	sig := r.ReadBytes(16)
	if err := r.Err(); err != nil {
		return p, err
	}

	pk := crypto.NewPubKey(AsymmetricMinKeySize)
	if err := pk.UnmarshalDER(der); err != nil {
		return p, err
	}

	if err := pk.Verify(sign, sig); err != nil {
		return p, err
	}

	p.forClient.pkey = pk
	p.rnd = rnd
	p.forClient.preMasterEnc = enc
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
	preMaster := s.forServer.preMaster
	if preMaster == nil {
		var err error
		preMaster, err = clientPrivate.Decrypt(s.forClient.preMasterEnc)
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
