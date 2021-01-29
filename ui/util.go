package ui

import (
	"strconv"
	"time"
)

func FormatDuration(d time.Duration, minParts int) (str string, parts int) {
	p := []int64{
		int64(d / time.Hour),
		int64(d / time.Minute),
		int64(d / time.Second),
	}
	p[2] -= p[1] * 60
	p[1] -= p[0] * 60

	l := make([]byte, 0, 3*minParts)
	for i := range p {
		if p[i] < 1 && i < 3-minParts {
			continue
		}
		parts++
		if p[i] < 10 {
			l = append(l, '0')
		}
		l = strconv.AppendInt(l, p[i], 10)
		l = append(l, ':')
	}

	if len(l) == 0 {
		return "", 0
	}

	return string(l[:len(l)-1]), parts
}

func StripUnprintable(str string) string {
	runes := make([]rune, 0, len(str))
	for _, n := range str {
		switch {
		case n == 9 || n == '\n':
		case n < 32:
			continue
		}
		runes = append(runes, n)
	}
	return string(runes)
}
