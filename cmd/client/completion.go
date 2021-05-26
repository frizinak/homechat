package main

import (
	"fmt"
	"sort"
	"strings"
)

func complete(input, prefix string, list []string, excludes map[string]struct{}) string {
	if len(input) == 0 {
		return ""
	}

	p := strings.Split(input, " ")
	l := p[len(p)-1]
	if len(l) < len(prefix) {
		return ""
	}

	if l[0:len(prefix)] != prefix {
		return ""
	}

	found := make([]string, 0)
	for _, n := range list {
		if _, ok := excludes[n]; ok {
			continue
		}

		if strings.HasPrefix(n, l[len(prefix):]) {
			found = append(found, n)
		}
	}

	if len(found) == 0 {
		return ""
	}

	var match string
	var shortest string
	sort.Slice(found, func(i, j int) bool {
		return len(found[i]) < len(found[j])
	})

	shortest, found = found[0], found[1:]
	for i := len(shortest); i >= 0; i-- {
		matchAll := true
		for _, f := range found {
			if f[:i] != shortest[:i] {
				matchAll = false
				break
			}
		}
		if matchAll {
			match = shortest[:i]
			break
		}
	}

	if match == "" {
		return ""
	}

	var suffix string
	if len(found) == 0 {
		suffix = " "
	}

	i := fmt.Sprintf(
		"%s%s%s",
		prefix,
		match,
		suffix,
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
