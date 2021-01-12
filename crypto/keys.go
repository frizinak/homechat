package crypto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
)

type Key struct {
	private crypto.PrivateKey
}

func NewKey() *Key {
	return &Key{}
}

func (k *Key) Generate() error {
	if k.private != nil {
		return nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	k.private = priv

	return nil
	// marsh, err := x509.MarshalECPrivateKey(priv)
	// if err != nil {
	// 	return err
	// }
}

func (k *Key) MarshalPEM(w io.Writer) error {
	return nil
}

func (k *Key) Marshal(w io.Writer) error {
	return nil
}
