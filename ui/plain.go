package ui

import (
	"fmt"
	"io"
	"time"
)

type PlainUI struct {
	io.Writer
}

func Plain(w io.Writer) *PlainUI {
	return &PlainUI{w}
}

func (p *PlainUI) Users([]string)          {}
func (p *PlainUI) UserTyping(string, bool) {}
func (p *PlainUI) Latency(time.Duration)   {}
func (p *PlainUI) Log(msg string)          { fmt.Fprintln(p.Writer, StripUnprintable(msg)) }
func (p *PlainUI) Err(err error)           { fmt.Fprintln(p.Writer, "[err]", StripUnprintable(err.Error())) }

func (p *PlainUI) Flash(msg string, dur time.Duration) {
	fmt.Fprintln(p.Writer, "[notice]", StripUnprintable(msg))
}

func (p *PlainUI) Clear()        {}
func (p *PlainUI) JumpToActive() {}
func (p *PlainUI) Broadcast(msgs []Msg, scroll bool) {
	for _, m := range msgs {
		p.broadcast(m)
	}
}

func (p *PlainUI) broadcast(msg Msg) {
	fmt.Fprintf(
		p.Writer,
		"%s: %s\n",
		StripUnprintable(msg.Meta),
		StripUnprintable(msg.Message),
	)
}

func (p *PlainUI) MusicState(s State) {}
