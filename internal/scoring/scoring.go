// Package scoring holds the shared definition of the validator score, so the
// spreadsheet writer and the web scorecard can never drift apart.
package scoring

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
)

// The Bilan tab: header on row 1, one validator per row from FirstRow to
// LastRow, columns A-L.
const (
	TabBilan = "Bilan"
	FirstRow = 2
	LastRow  = 99
	NumRows  = LastRow - FirstRow + 1
)

// Bilan columns, by A1 letter.
const (
	ColMoniker    = "A"
	ColDiscord    = "B"
	ColChallenge  = "C"
	ColTest13     = "D"
	ColTopaz      = "E"
	ColSapphire   = "F"
	ColPearl      = "G"
	ColFireDrill  = "H"
	ColGitHub     = "I"
	ColScore      = "J"
	ColGitHubUser = "K" // added by this tool
	ColConfidence = "L" // added by this tool
)

// Network tabs, in the order they are presented.
var Networks = []string{"Score-test13", "Score-topaz", "Score-sapphire", "Score-pearl"}

// Non-network tabs hold rubric breakdowns rather than gnomonitoring metrics.
const (
	TabChallenge = "Test13-challenge"
	TabFireDrill = "Score-firedrill"
)

// The scoring rule.
//
//	Score = ( challenge + test13 + topaz + sapphire + pearl + fire drill ) / ArenaDivisor
//	      + GitHub / BonusDivisor,  capped at MaxScore
//
// The six arenas carry equal weight, so each contributes at most 100/6 points.
// GitHub is a bonus on top worth at most 10: it can lift a candidate, never
// sink one. The cap matters — without it the formula reaches 110.
const (
	ArenaDivisor = 6
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

// ScoreFormula is the capped total for one Bilan row, in fr_FR syntax.
func ScoreFormula(row int) string {
	return fmt.Sprintf("=MIN(%d; (%s%d+%s%d+%s%d+%s%d+%s%d+%s%d)/%d + %s%d/%d)",
		MaxScore,
		ColChallenge, row, ColTest13, row, ColTopaz, row, ColSapphire, row, ColPearl, row, ColFireDrill, row,
		ArenaDivisor, ColGitHub, row, BonusDivisor)
}

// Link ties a validator to the GitHub account operating it.
type Link struct {
	Moniker    string
	Discord    string
	Login      string
	Confidence string  // certain | strong | weak
	Score      float64 // participation score, contributions from CutoffYear on
}

// LoadLinks reads data/github-links.csv.
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
		if cur, ok := out[l.Moniker]; !ok || rank[l.Confidence] < rank[cur.Confidence] {
			out[l.Moniker] = l
		}
	}
	return out, nil
}
