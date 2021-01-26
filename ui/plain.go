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

func (p *PlainUI) Users(msg string)                    {}
func (p *PlainUI) Latency(latency time.Duration)       {}
func (p *PlainUI) Log(msg string)                      { fmt.Fprintln(p.Writer, "[log]", msg) }
func (p *PlainUI) Flash(msg string, dur time.Duration) { fmt.Fprintln(p.Writer, "[notice]", msg) }
func (p *PlainUI) Err(err error)                       { fmt.Fprintln(p.Writer, "[err]", err) }
func (p *PlainUI) Clear()                              {}
func (p *PlainUI) JumpToActive()                       {}
func (p *PlainUI) Broadcast(msgs []Msg, scroll bool) {
	for _, m := range msgs {
		p.broadcast(m)
	}
}

func (p *PlainUI) broadcast(msg Msg) {
	fmt.Fprintf(
		p.Writer,
		"%s %-15s: %s\n",
		msg.Stamp.Format("2006-01-02 15:04:05"),
		msg.From,
		msg.Message,
	)
}

func (p *PlainUI) MusicState(s State) {}
