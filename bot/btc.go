package bot

import (
	"fmt"
	"strings"

	"github.com/frizinak/bitstamp"
	"github.com/frizinak/bitstamp/api"
	"github.com/frizinak/bitstamp/generic"
)

const BTCName = "btc-bot"

type BTCBot struct {
	client *bitstamp.Bitstamp
}

func NewBTCBot() (*BTCBot, error) {
	btc, err := bitstamp.NewDefaults("", "")
	return &BTCBot{btc}, err
}

func (btc *BTCBot) Message(user string, args ...string) (string, string, error) {
	pair := generic.CurrencyPair{bitstamp.BTC, bitstamp.EUR}
	arg := ""
	if len(args) > 0 {
		arg = strings.ToLower(args[0])
	}
	switch arg {
	case "help":
		out := []string{
			" - usd",
			" - eur",
			" - gbp",
		}
		return BTCName, strings.Join(out, "\n"), nil
	case "eur", "euro", "euros":
	case "usd", "dollar", "dollars", "$":
		pair.Counter = bitstamp.USD
	case "gbp", "pound", "pounds":
		pair.Counter = bitstamp.GBP
	}

	res, err := btc.client.API.Ticker(pair, api.TickerHourly)
	if err != nil {
		return BTCName, err.Error(), nil
	}

	out := fmt.Sprintf(
		"%s %.2f [VWAP: %.2f]",
		strings.ToUpper(pair.Counter.String()),
		res.Last,
		res.VWAP,
	)
	return BTCName, out, nil
}
