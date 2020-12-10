package bot

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

const HolidayName = "holiday-bot"

type HolidayBot struct {
	countryCode string
}

func NewHolidayBot(countryCode string) *HolidayBot {
	return &HolidayBot{countryCode}
}

func (h *HolidayBot) Message(user string, args ...string) (string, string, error) {
	type r struct {
		Date      string `json:"date"`
		LocalName string `json:"localName"`
		Name      string `json:"name"`
	}

	year := time.Now().Format("2006")
	country := "UZ"
	if h.countryCode != "" {
		country = h.countryCode
	}

	b := bytes.NewBuffer(nil)
	if len(args) > 0 && args[0] == "help" {
		fmt.Fprintf(b, " - <year> <countryCode>\n")
		fmt.Fprintf(b, "       <year>:         the year (%s)\n", year)
		fmt.Fprintf(b, "       <countryCode>:  2-letter country code (UZ)")

		return HolidayName, b.String(), nil
	}

	if len(args) > 0 {
		year = args[0]
	}
	if len(args) > 1 {
		country = args[1]
	}

	_result, err := simpleAPI(
		fmt.Sprintf(
			"https://date.nager.at/api/v2/PublicHolidays/%s/%s",
			year,
			strings.ToUpper(country),
		),
		&[]r{},
	)
	if err != nil {
		return ProgrammingQuoteName, "", err
	}
	results := _result.(*[]r)

	format := " - %s) %-20s (%s)"
	formatNL := format + "\n"
	for i, r := range *results {
		f := formatNL
		if i == len(*results)-1 {
			f = format
		}

		fmt.Fprintf(
			b,
			f,
			strings.TrimSpace(r.Date),
			strings.TrimSpace(r.LocalName),
			strings.TrimSpace(r.Name),
		)
	}

	return HolidayName, b.String(), nil
}
