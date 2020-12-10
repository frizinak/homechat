package bot

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

const (
	WttrName = "wttr-bot"
)

type WttrBot struct {
	city string
}

func NewWttrBot(defaultCity string) *WttrBot {
	return &WttrBot{defaultCity}
}

func (w *WttrBot) Message(user string, args ...string) (string, string, error) {
	city := w.city
	if len(args) > 0 {
		city = args[0]
	}
	res, err := http.Get(fmt.Sprintf("http://wttr.in/%s?A0qnT", strings.ToLower(city)))
	if err != nil {
		return WttrName, "", err
	}
	defer res.Body.Close()
	d, err := ioutil.ReadAll(res.Body)
	return WttrName, string(d), err
}
