package ui

import (
	"image"
	"time"

	"github.com/frizinak/zug"
	"github.com/frizinak/zug/cli"
	"github.com/frizinak/zug/img"
)

type Zug interface {
	IsNOOP() bool
	Layer(string) Layer
	Render() error
}

type Layer interface {
	Hide()
	Show()
	SetSource(string) error
	Set(x, y, width, height int)
	QueueDraw()
	Geometry(image.Point) (image.Rectangle, error)
}

type zugWrap struct {
	noop bool
	*zug.Zug
}

type layerWrap struct {
	*zug.Layer
}

func (ui *TermUI) newZug(bin string) Zug {
	if bin == "" {
		return &zugWrap{noop: true}
	}

	uzug := cli.New(cli.Config{
		UeberzugBinary: bin,
		OnError: func(err error) {
			ui.Flash(err.Error(), time.Second*5)
		},
	})

	if err := uzug.Init(); err != nil {
		ui.Flash(err.Error(), time.Second*5)
		return &zugWrap{noop: true}
	}

	return &zugWrap{noop: false, Zug: zug.New(img.DefaultManager, uzug)}
}

func (z *zugWrap) IsNOOP() bool { return z.noop }

func (z *zugWrap) Layer(name string) Layer {
	if z.Zug == nil {
		return &layerWrap{}
	}
	l := z.Zug.Layer(name)
	return &layerWrap{l}
}

func (l *layerWrap) Set(x, y, w, h int) {
	l.X, l.Y, l.Width, l.Height = x, y, w, h
}
