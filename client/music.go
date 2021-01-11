package client

import (
	"encoding/json"
	"io"

	"github.com/frizinak/binary"
	"github.com/frizinak/homechat/server/channel"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
)

type MusicState struct {
	musicdata.ServerStateMessage
	musicdata.ServerSongMessage
}

func (m MusicState) Binary(w *binary.Writer) error {
	if err := m.ServerStateMessage.Binary(w); err != nil {
		return err
	}
	return m.ServerSongMessage.Binary(w)
}

func (m MusicState) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m MusicState) Equal(msg channel.Msg) bool { return m == msg }

func (m MusicState) FromBinary(r *binary.Reader) (channel.Msg, error) {
	msg, err := m.ServerSongMessage.FromBinary(r)
	if err != nil {
		return m, err
	}
	m.ServerSongMessage = msg.(musicdata.ServerSongMessage)

	msg, err = m.ServerStateMessage.FromBinary(r)
	if err != nil {
		return m, err
	}
	m.ServerStateMessage = msg.(musicdata.ServerStateMessage)
	return m, nil
}

func (m MusicState) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}