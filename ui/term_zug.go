package ui

import (
	"github.com/frizinak/zug"
	"github.com/frizinak/zug/img"
	"github.com/frizinak/zug/x"
)

type Zug struct {
	*zug.Zug
	noop bool
}

func (ui *TermUI) newZug(noop bool) *Zug {
	if noop {
		return &Zug{noop: true}
	}

	term, err := x.NewFromEnv()
	if err != nil {
		return &Zug{noop: true}
	}

	return &Zug{noop: false, Zug: zug.New(img.DefaultManager, term)}
}

func (z *Zug) IsNOOP() bool { return z.noop }
