// Package scoring holds the shared definition of the validator score, so the
// spreadsheet writer and the web scorecard can never drift apart.
package scoring

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The Bilan tab: header on row 1, one validator per row from FirstRow on.
// Its columns follow config/arenas.json; `sheet push` lays them out.
const (
	TabBilan = "Bilan"
	FirstRow = 2
)

// Network tabs, in the order they are presented.
var Networks = []string{"Score-test13", "Score-topaz", "Score-sapphire", "Score-pearl"}

// Non-network tabs hold rubric breakdowns rather than gnomonitoring metrics.
const (
	TabChallenge = "Test13-challenge"
	TabFireDrill = "Score-firedrill"
)

// FrozenTabs are the arenas whose scores are final. The testnets are over and
// the fire drill has closed, so nothing may rewrite them: a refresh would only
// replace a final result with whatever the API serves today, or with nothing.
// Mainnet is the one arena expected to move, and it is not scored yet.
var FrozenTabs = append([]string{TabChallenge, TabFireDrill}, Networks...)

// IsFrozen reports whether a tab holds a final arena result.
func IsFrozen(tab string) bool {
	for _, t := range FrozenTabs {
		if strings.EqualFold(t, tab) {
			return true
		}
	}
	return false
}

// Key is how every join matches a moniker: trimmed and case-insensitive, the
// way Bilan's VLOOKUP does. A tab can spell a validator "offellnode" where
// Bilan says "offellNode", and the address column is useless as a key because
// validators rotate keys between chains.
func Key(moniker string) string {
	return strings.ToLower(strings.TrimSpace(moniker))
}

// The scoring rule.
//
//	Score = ( sum of the arenas in the total ) / their count
//	      + GitHub / BonusDivisor,  capped at MaxScore
//
// The arenas, and which count toward the total, are listed in
// config/arenas.json. They carry equal weight. GitHub is a bonus on top worth
// at most 10: it can lift a candidate, never sink one. The cap matters —
// without it the formula reaches 110.
const (
	BonusDivisor = 10
	MaxScore     = 100
)

// GitHub participation weights. A merged pull request is the only artefact
// that proves code landed, so it outweighs intent and discussion by a wide
// margin.
const (
	WeightMergedPR   = 25
	WeightUnmergedPR = 8
	WeightIssue      = 5
	WeightCommit     = 3
	WeightComment    = 2
)

// CutoffYear is the earliest year of GitHub activity that counts toward the
// bonus. Contributions predating test13 are real, but they say nothing about
// a candidate's involvement in the testnet being scored.
const CutoffYear = 2025

// Link ties a validator to the GitHub account operating it.
type Link struct {
	Moniker    string
	Discord    string
	Login      string
	Confidence string  // certain | strong | weak
	Score      float64 // participation score, contributions from CutoffYear on
}

// LoadLinks reads data/github-links.csv, keyed by Key(moniker).
//
// A moniker can carry several accounts: AviaOne has the brand account plus a
// team member. Only a non-weak row is the operator's own identity; weak rows
// are same-organisation colleagues and must never feed the score.
func LoadLinks(path string) (map[string]Link, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%s: no data rows", path)
	}

	rank := map[string]int{"certain": 0, "strong": 1, "weak": 2}
	out := map[string]Link{}
	for _, r := range rows[1:] {
		if len(r) < 5 {
			continue
		}
		if r[3] == "weak" {
			continue
		}
		score, err := strconv.ParseFloat(r[4], 64)
		if err != nil {
			return nil, fmt.Errorf("%s: bad score %q for %s", path, r[4], r[0])
		}
		l := Link{Moniker: r[0], Discord: r[1], Login: r[2], Confidence: r[3], Score: score}
		if cur, ok := out[Key(l.Moniker)]; !ok || rank[l.Confidence] < rank[cur.Confidence] {
			out[Key(l.Moniker)] = l
		}
	}
	return out, nil
}
