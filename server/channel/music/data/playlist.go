package data

import (
	"encoding/json"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
)

type ServerPlaylistMessage struct {
	List []string `json:"list"`
}

func (m ServerPlaylistMessage) Equal(msg channel.Msg) bool {
	m1, ok := msg.(ServerPlaylistMessage)
	if !ok {
		return false
	}

	if len(m.List) != len(m1.List) {
		return false
	}

	for i := range m.List {
		if m.List[i] != m1.List[i] {
			return false
		}
	}

	return true
}

func (m ServerPlaylistMessage) Binary(w *binary.Writer) error {
	w.WriteUint32(uint32(len(m.List)))
	for _, p := range m.List {
		w.WriteString(p, 16)
	}
	return w.Err()
}

func (m ServerPlaylistMessage) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m ServerPlaylistMessage) FromBinary(r *binary.Reader) (channel.Msg, error) {
	return BinaryServerPlaylistMessage(r)
}

func (m ServerPlaylistMessage) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	return JSONServerPlaylistMessage(r)
}

func BinaryServerPlaylistMessage(r *binary.Reader) (ServerPlaylistMessage, error) {
	c := ServerPlaylistMessage{}
	n := r.ReadUint32()
	c.List = make([]string, n)
	var i uint32
	for ; i < n; i++ {
		c.List[i] = r.ReadString(16)
	}
	return c, r.Err()
}

func JSONServerPlaylistMessage(r io.Reader) (ServerPlaylistMessage, io.Reader, error) {
	c := ServerPlaylistMessage{}
	nr, err := channel.JSON(r, &c)
	return c, nr, err
}