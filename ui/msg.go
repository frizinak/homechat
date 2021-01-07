package ui

import "time"

type Highlight byte

const (
	HLNone Highlight = iota
	HLTitle
	HLActive
	HLMuted
	HLSlight
)

type Msg struct {
	From      string
	Stamp     time.Time
	Message   string
	Notify    bool
	Highlight Highlight
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
