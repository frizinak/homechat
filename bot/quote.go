package bot

import (
	"fmt"
)

const (
	ProgrammingQuoteName = "prog-quote-bot"
	CatQuoteName         = "cat-quote-bot"
)

func ProgrammingQuote(user string, args ...string) (string, string, error) {
	type r struct {
		Author string `json:"author"`
		Quote  string `json:"quote"`
	}

	_result, err := simpleAPI("http://quotes.stormconsultancy.co.uk/random.json", &r{})
	if err != nil {
		return ProgrammingQuoteName, "", err
	}
	result := _result.(*r)
	q := fmt.Sprintf("%s\n    -%s", result.Quote, result.Author)
	return ProgrammingQuoteName, q, nil
}

func CatQuote(user string, args ...string) (string, string, error) {
	type r struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	}

	_result, err := simpleAPI("https://cat-fact.herokuapp.com/facts/random", &r{})
	if err != nil {
		return CatQuoteName, "", err
	}
	result := _result.(*r)
	q := fmt.Sprintf("%s\n    -%s", result.Text, result.Source)
	return CatQuoteName, q, nil
}
