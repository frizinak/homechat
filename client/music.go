package client

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/frizinak/homechat/server/channel"
	musicdata "github.com/frizinak/homechat/server/channel/music/data"
	"github.com/frizinak/homechat/ui"
)

type MusicState struct {
	musicdata.ServerStateMessage
	musicdata.ServerSongMessage
}

func (m MusicState) Binary(w channel.BinaryWriter) error {
	if err := m.ServerStateMessage.Binary(w); err != nil {
		return err
	}
	return m.ServerSongMessage.Binary(w)
}

func (m MusicState) JSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(m)
}

func (m MusicState) Equal(msg channel.Msg) bool { return m == msg }
func (m MusicState) Close() error               { return nil }

func (m MusicState) FromBinary(r channel.BinaryReader) (channel.Msg, error) {
	msg, err := m.ServerStateMessage.FromBinary(r)
	if err != nil {
		return m, err
	}
	m.ServerStateMessage = msg.(musicdata.ServerStateMessage)

	msg, err = m.ServerSongMessage.FromBinary(r)
	if err != nil {
		return m, err
	}
	m.ServerSongMessage = msg.(musicdata.ServerSongMessage)

	return m, nil
}

func (m MusicState) FromJSON(r io.Reader) (channel.Msg, io.Reader, error) {
	nr, err := channel.JSON(r, &m)
	return m, nr, err
}

func (m MusicState) Format(delimiter string) string {
	dur, p := ui.FormatDuration(m.Duration, 3)
	pos, _ := ui.FormatDuration(m.Position, p)
	var pct float64
	if m.Duration > 0 {
		pct = float64(m.Position/time.Second) /
			float64(m.Duration/time.Second)
	}
	if pct > 1 {
		pct = 1
	}

	pause := "play"
	if m.Paused {
		pause = "pause"
	}

	return fmt.Sprintf(
		"%s:%s%s%s%s%s%s%s|%s%s%d%s%d",
		m.Song.NS(),
		m.Song.ID(),
		delimiter,
		ui.StripUnprintable(m.Song.Title()),
		delimiter,
		pause,
		delimiter,
		pos,
		dur,
		delimiter,
		int(100*pct),
		delimiter,
		int(100*m.Volume),
	)
}
