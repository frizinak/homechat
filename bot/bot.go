package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

var ErrNotExists = errors.New("no such bot")

type Messager func(user string, args ...string) (string, string, error)

type Bot interface {
	Message(user string, args ...string) (string, string, error)
}

func NewBotFunc(f Messager) Bot { return &BotFunc{f} }

type BotFunc struct{ handler Messager }

func (f *BotFunc) Message(user string, args ...string) (string, string, error) {
	return f.handler(user, args...)
}

type BotCollection struct {
	name string
	bots map[string]Bot
}

func NewBotCollection(name string) *BotCollection {
	return &BotCollection{name, make(map[string]Bot)}
}

func (c *BotCollection) AddBot(command string, bot Bot) {
	c.bots[command] = bot
}

func (c *BotCollection) Commands() string {
	list := make([]string, 0, len(c.bots))
	for n := range c.bots {
		list = append(list, fmt.Sprintf("  - %s", n))
	}

	sort.Strings(list)
	return strings.Join(list, "\n")
}

func (c *BotCollection) Message(user string, args ...string) (string, string, error) {
	if len(args) < 1 || args[0] == "help" || args[0] == "list" {
		return c.name, c.Commands(), nil
	}

	bot := c.bots[args[0]]
	if bot == nil {
		return c.name, "unknown command", nil
	}

	return bot.Message(user, args[1:]...)
}

func simpleAPI(endpoint string, dataType interface{}) (interface{}, error) {
	res, err := http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	return dataType, dec.Decode(dataType)
}
