package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const (
	reasonNotRegistered = "not registered in r/gnops/valopers"
	reasonAmbiguous     = "several valopers share this moniker"
)

type unmatchedCandidate struct {
	Moniker string
	Reason  string
}

// normMoniker is how publish matches a scorecard moniker to a valoper:
// case-insensitive, surrounding spaces dropped, runs of spaces collapsed.
func normMoniker(s string) string { return strings.ToLower(collapseSpaces(s)) }

// loadValoperMap reads the hand-kept moniker,operator_address overrides. A
// missing file means no override.
func loadValoperMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) == 0 || len(rows[0]) != 2 || rows[0][0] != "moniker" || rows[0][1] != "operator_address" {
		return nil, fmt.Errorf("%s: the header must be moniker,operator_address", path)
	}
	out := map[string]string{}
	for i, r := range rows[1:] {
		line := i + 2
		k, op := normMoniker(r[0]), strings.TrimSpace(r[1])
		if k == "" {
			return nil, fmt.Errorf("%s line %d: empty moniker", path, line)
		}
		if !operatorRe.MatchString(op) {
			return nil, fmt.Errorf("%s line %d: %q is not a g1 address", path, line, op)
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s line %d: %s is listed twice", path, line, r[0])
		}
		out[k] = op
	}
	return out, nil
}

// matchOperators resolves each scorecard moniker to a registered operator
// address. An override wins; otherwise the moniker must match exactly one
// valoper. Nothing is guessed: a candidate without a single match is
// reported, not published. The returned map is keyed by normMoniker.
func matchOperators(monikers []string, roster []valoper, overrides map[string]string) (map[string]string, []unmatchedCandidate, error) {
	registered := map[string]bool{}
	byMoniker := map[string][]string{}
	for _, v := range roster {
		registered[v.Operator] = true
		k := normMoniker(v.Moniker)
		byMoniker[k] = append(byMoniker[k], v.Operator)
	}
	candidates := map[string]bool{}
	for _, m := range monikers {
		k := normMoniker(m)
		if candidates[k] {
			return nil, nil, fmt.Errorf("the scorecard lists %q twice", m)
		}
		candidates[k] = true
	}
	for k, op := range overrides {
		if !candidates[k] {
			return nil, nil, fmt.Errorf("valoper map: %q is not a scorecard candidate", k)
		}
		if !registered[op] {
			return nil, nil, fmt.Errorf("valoper map: %s (%s) has no valoper record on this chain", op, k)
		}
	}
	matched := map[string]string{}
	owner := map[string]string{}
	var unmatched []unmatchedCandidate
	for _, m := range monikers {
		k := normMoniker(m)
		op, ok := overrides[k]
		if !ok {
			switch ops := byMoniker[k]; len(ops) {
			case 0:
				unmatched = append(unmatched, unmatchedCandidate{m, reasonNotRegistered})
				continue
			case 1:
				op = ops[0]
			default:
				unmatched = append(unmatched, unmatchedCandidate{m, reasonAmbiguous})
				continue
			}
		}
		if prev, taken := owner[op]; taken {
			return nil, nil, fmt.Errorf("%s and %s resolve to the same operator %s", prev, m, op)
		}
		owner[op] = m
		matched[k] = op
	}
	return matched, unmatched, nil
}
