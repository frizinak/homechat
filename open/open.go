package open

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type OpenCB func(what string) error

type Opener struct {
	open    OpenCB
	openURL OpenCB
}

func New() *Opener { return &Opener{open, openURL} }

func (o *Opener) Open(url string) error    { return o.open(url) }
func (o *Opener) OpenURL(url string) error { return o.openURL(url) }
func (o *Opener) SetOpen(cb OpenCB)        { o.open = cb }
func (o *Opener) SetOpenURL(cb OpenCB)     { o.openURL = cb }

func Run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	buf := bytes.NewBuffer(nil)
	bufe := bytes.NewBuffer(nil)
	c.Stdout = buf
	c.Stderr = bufe
	if err := c.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(bufe.String()))
	}

	return nil
}
