package handler

import (
	"errors"
	"io"
	"io/ioutil"

	"github.com/frizinak/homechat/client"
	updatedata "github.com/frizinak/homechat/server/channel/update/data"
	"github.com/frizinak/homechat/vars"
)

var ErrNotExist = errors.New("does not exist")

type UpdateHandler struct {
	client.Handler
	log client.Logger
	cl  *client.Client

	w    io.Writer
	done chan error
}

func NewUpdateHandler(
	handler client.Handler,
	log client.Logger,
	cl *client.Client,
) *UpdateHandler {
	d := &UpdateHandler{
		Handler: handler,
		log:     log,
		cl:      cl,
	}

	return d
}

func (u *UpdateHandler) Download(os, arch string, w io.Writer) error {
	u.done = make(chan error)
	u.w = w
	err := u.cl.Send(vars.UpdateChannel, updatedata.Message{GOOS: os, GOARCH: arch})
	if err != nil {
		return err
	}

	return <-u.done
}

func (u *UpdateHandler) HandleUpdateMessage(m updatedata.ServerMessage) error {
	u.done <- func() error {
		if !m.Available {
			return ErrNotExist
		}

		k := u.cl.ServerKey()
		d, err := ioutil.ReadAll(m.Upload())
		if err != nil {
			return err
		}
		if err := k.Verify(d, m.Sig); err != nil {
			return err
		}
		_, err = u.w.Write(d)
		return err
	}()

	return nil
}
