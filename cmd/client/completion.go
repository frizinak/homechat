package main

import (
	"fmt"
	"strings"
)

func complete(input, prefix string, list []string, excludes map[string]struct{}) string {
	if len(input) == 0 {
		return ""
	}

	p := strings.Split(input, " ")
	l := p[len(p)-1]
	if l[0:len(prefix)] != prefix {
		return ""
	}

	found := ""
	foundC := 0
	for _, n := range list {
		if _, ok := excludes[n]; ok {
			continue
		}

		if strings.HasPrefix(n, l[len(prefix):]) {
			found = n
			if foundC++; foundC > 1 {
				return ""
			}
		}
	}

	if foundC != 1 {
		return ""
	}
	i := fmt.Sprintf(
		"%s%s ",
		prefix,
		found,
	)
	if len(p) > 1 {
		i = fmt.Sprintf(
			"%s %s",
			strings.Join(p[:len(p)-1], " "),
			i,
		)
	}

	return i
}
