package ui

import (
	"fmt"
	"io"
)

type PlainUI struct {
	io.Writer
}

func Plain(w io.Writer) *PlainUI {
	return &PlainUI{w}
}

func (p *PlainUI) Users(msg string) {}
func (p *PlainUI) Log(msg string)   {}
func (p *PlainUI) Flash(msg string) { fmt.Fprintln(p.Writer, "FLASH", msg) }
func (p *PlainUI) Err(err string)   { fmt.Fprintln(p.Writer, "ERR", err) }
func (p *PlainUI) Clear()           {}
func (p *PlainUI) BroadcastMulti(msgs []Msg, scroll bool) {
	for _, m := range msgs {
		p.Broadcast(m, scroll)
	}
}

func (p *PlainUI) Broadcast(msg Msg, scroll bool) {
	fmt.Fprintf(
		p.Writer,
		"%s %-15s: %s\n",
		msg.Stamp.Format("2006-01-02 15:04:05"),
		msg.From,
		msg.Message,
	)
}

func (p *PlainUI) MusicState(s State) {}
