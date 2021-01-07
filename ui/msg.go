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
	Pos      float64
	Duration time.Duration
	Volume   float64
}
