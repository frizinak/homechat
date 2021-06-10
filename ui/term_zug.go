package ui

import (
	"image"

	"github.com/frizinak/zug"
	"github.com/frizinak/zug/img"
	"github.com/frizinak/zug/x"
)

type ZugDimensions struct {
	W, H int
}

type ZugGeometry struct {
	Image  ZugDimensions
	Window ZugDimensions
}

type Zug interface {
	IsNOOP() bool
	Layer(string) Layer
	Render() error
}

type Layer interface {
	Hide()
	Show()
	SetSource(string) error
	Render()
	SetGeometryTerminal(image.Rectangle) error
	GeometryTerminal() (ZugGeometry, error)
}

type zugWrap struct {
	noop bool
	*zug.Zug
}

type layerWrap struct {
	*zug.Layer
}

func (ui *TermUI) newZug(noop bool) Zug {
	if noop {
		return &zugWrap{noop: true}
	}

	term, err := x.NewFromEnv()
	if err != nil {
		return &zugWrap{noop: true}
	}

	return &zugWrap{noop: false, Zug: zug.New(img.DefaultManager, term)}
}

func (z *zugWrap) IsNOOP() bool { return z.noop }

func (z *zugWrap) Layer(name string) Layer {
	if z.Zug == nil {
		return &layerWrap{}
	}
	l := z.Zug.Layer(name)
	return &layerWrap{l}
}

func (l *layerWrap) GeometryTerminal() (ZugGeometry, error) {
	geom, err := l.Layer.GeometryTerminal()
	g := ZugGeometry{
		Image:  ZugDimensions{W: geom.Image.W, H: geom.Image.H},
		Window: ZugDimensions{W: geom.Window.W, H: geom.Image.H},
	}

	return g, err
}
