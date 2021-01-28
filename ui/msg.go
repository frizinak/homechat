package ui

import (
	"time"

	chatdata "github.com/frizinak/homechat/server/channel/chat/data"
)

type Highlight byte

const (
	HLNone Highlight = 1 << iota
	HLTitle
	HLActive
	HLMuted
	HLSlight
	HLProblem
	HLTemporary
)

type Msg struct {
	From      string
	Stamp     time.Time
	Message   string
	Notify    chatdata.Notify
	Highlight Highlight
}

func (m Msg) NotifyPersonal() bool {
	return m.Notify&chatdata.NotifyPersonal != 0
}

func (m Msg) NotifyNever() bool {
	return m.Notify&chatdata.NotifyNever != 0
}

type State struct {
	Song     string
	Paused   bool
	Position time.Duration
	Duration time.Duration
	Volume   float64
}

func (s State) Pos() float64 {
	if s.Duration < time.Second {
		return 0
	}
	return float64(s.Position/time.Second) / float64(s.Duration/time.Second)
}
