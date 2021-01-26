package music

import "github.com/frizinak/homechat/client"

type ErrorFlasher struct {
	l client.Logger
}

func NewErrorFlasher(l client.Logger) *ErrorFlasher { return &ErrorFlasher{l} }

func (e *ErrorFlasher) Err(err error) { e.l.Flash(err.Error(), 0) }
