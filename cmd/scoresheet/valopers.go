package main

import (
	"fmt"
	"regexp"
	"strings"
)

// renderer reads a realm's Render output, as gnoweb shows it.
type renderer interface {
	Render(pkgPath, path string) (string, error)
}

// valopersRealm is the validator profile registry. publish keys every
// candidate by the operator address registered there.
const valopersRealm = "gno.land/r/gnops/valopers"

// maxValoperPages bounds the walk: 50 pages of 50 valopers.
const maxValoperPages = 50

type valoper struct {
	Moniker  string
	Operator string
}

// valoperLineRe matches one roster entry of Render(""), a Markdown list item
// linking [Moniker](/r/gnops/valopers:g1op) followed by a profile link.
// The same shape Memba parses (frontend/src/lib/valopers.ts). Instruction
// text and the pager do not match it.
var valoperLineRe = regexp.MustCompile(`\*\s+\[([^\]]+)\]\(/r/gnops/valopers:(g1[a-z0-9]+)\)`)

func parseValoperList(raw string) []valoper {
	var out []valoper
	for _, m := range valoperLineRe.FindAllStringSubmatch(raw, -1) {
		out = append(out, valoper{Moniker: strings.TrimSpace(m[1]), Operator: m[2]})
	}
	return out
}

// fetchValopers reads the whole roster. The realm pages it 50 at a time and
// links ?page=N+1 while a next page exists; reading page 1 alone would cap
// the roster at 50 without a word. A page that is advertised but empty, or
// that adds nothing new, means the read is incomplete, not finished.
func fetchValopers(r renderer) ([]valoper, error) {
	var out []valoper
	seen := map[string]bool{}
	for page := 1; page <= maxValoperPages; page++ {
		path := ""
		if page > 1 {
			path = fmt.Sprintf("?page=%d", page)
		}
		raw, err := r.Render(valopersRealm, path)
		if err != nil {
			return nil, fmt.Errorf("valopers page %d: %w", page, err)
		}
		if raw == "" {
			if page > 1 {
				return nil, fmt.Errorf("valopers page %d is empty after being advertised", page)
			}
			return out, nil
		}
		added := 0
		for _, v := range parseValoperList(raw) {
			if !seen[v.Operator] {
				seen[v.Operator] = true
				out = append(out, v)
				added++
			}
		}
		next := strings.Contains(raw, fmt.Sprintf("?page=%d", page+1))
		if added == 0 {
			if page > 1 || next {
				return nil, fmt.Errorf("valopers page %d repeats or holds no entry", page)
			}
			return out, nil
		}
		if !next {
			return out, nil
		}
	}
	return nil, fmt.Errorf("valopers registry exceeds %d pages", maxValoperPages)
}
