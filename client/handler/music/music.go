package music

import (
	"github.com/frizinak/homechat/client"
)

type ErrorFlasher struct {
	l client.Logger
}

func NewErrorFlasher(l client.Logger) *ErrorFlasher { return &ErrorFlasher{l} }

func (e *ErrorFlasher) Err(err error) { e.l.Flash(err.Error(), 0) }

type CurrentSongHandler struct {
	client.Handler
	updates chan<- client.MusicState
}

func NewCurrentSongHandler(handler client.Handler, updates chan<- client.MusicState) *CurrentSongHandler {
	return &CurrentSongHandler{handler, updates}
}

func (h *CurrentSongHandler) HandleMusicStateMessage(m client.MusicState) error {
	h.updates <- m
	return nil
}
