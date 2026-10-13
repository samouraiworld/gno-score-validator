package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// details is what the scorecard shows beside each arena score: the challenge
// and fire drill rubrics, and the onchain metrics of every network. Each map is
// keyed by scoring.Key(moniker).
type details struct {
	challenge map[string]snapChallenge
	firedrill map[string]snapFireDrill
	onchain   map[string]map[string]snapOnchain // arena id -> moniker -> row
}

// loadDetails reads the detail of every arena that carries one. rows returns an
// arena's data rows, header excluded, as text: the CSV under data/arenas/ for
// `score`, the tab itself for `snapshot`. Both go through the same parsers, so
// a frozen CSV and the tab it was frozen from describe a validator the same way.
func loadDetails(cfg arena.Config, rows func(arena.Arena) ([][]string, error)) (details, error) {
	d := details{
		challenge: map[string]snapChallenge{},
		firedrill: map[string]snapFireDrill{},
		onchain:   map[string]map[string]snapOnchain{},
	}
	for _, a := range cfg.Arenas {
		if a.Detail == "" {
			continue
		}
		rs, err := rows(a)
		if err != nil {
			return d, err
		}
		switch a.Detail {
		case arena.DetailChallenge:
			d.challenge = parseChallenge(rs)
		case arena.DetailFireDrill:
			d.firedrill = parseFireDrill(rs)
		case arena.DetailOnchain:
			d.onchain[a.ID] = parseOnchain(rs)
		default:
			return d, fmt.Errorf("arena %q: unknown detail %q", a.ID, a.Detail)
		}
	}
	return d, nil
}

// parseChallenge reads the onboarding breakdown: columns Candidate, Moniker,
// Weighted, Simple, Penalty, Bonus, Final (w), Score.
func parseChallenge(rows [][]string) map[string]snapChallenge {
	out := map[string]snapChallenge{}
	for _, r := range rows {
		k := scoring.Key(text(r, 1))
		if k == "" {
			continue
		}
		out[k] = snapChallenge{
			Weighted: textNumber(r, 2), Simple: textNumber(r, 3), Penalty: textNumber(r, 4),
			Bonus: textNumber(r, 5), FinalW: textNumber(r, 6), Score: textNumber(r, 7),
		}
	}
	return out
}

// parseFireDrill reads the exercise rubric. Portal spelling and the rotated
// exercise address are kept for provenance, never for joining.
func parseFireDrill(rows [][]string) map[string]snapFireDrill {
	out := map[string]snapFireDrill{}
	for _, r := range rows {
		k := scoring.Key(text(r, 0))
		if k == "" {
			continue
		}
		out[k] = snapFireDrill{
			UploadTime: textOptNumber(r, 3), Metadata: textOptNumber(r, 4), LogQuality: textOptNumber(r, 5),
			Incident: textNumber(r, 6), Findings: text(r, 7),
			PortalMoniker: text(r, 1), Address: text(r, 8),
		}
	}
	return out
}

// parseOnchain reads one network's gnomonitoring metrics. A tab can spell a
// validator "offellnode" where the candidate list says "offellNode", hence
// the Key.
func parseOnchain(rows [][]string) map[string]snapOnchain {
	out := map[string]snapOnchain{}
	for _, r := range rows {
		k := scoring.Key(text(r, 0))
		if k == "" {
			continue
		}
		// VLOOKUP answers with the first match, so the detail must too, or
		// the page would describe a different node than the one scored.
		if _, dup := out[k]; dup {
			continue
		}
		out[k] = snapOnchain{
			Address: text(r, 1), Score: textNumber(r, 2), Tier: text(r, 3),
			Sign: textNumber(r, 4), VP: textNumber(r, 5),
			// A blank proposer means the validator was never asked to
			// propose. The page renders null as an em dash; a zero would read
			// as failure.
			Proposer: textOptNumber(r, 6),
			Critical: textNumber(r, 7), Warning: textNumber(r, 8), Freq: textNumber(r, 9),
			FreqRate: textNumber(r, 10), Downtime: textNumber(r, 11), Missed: textNumber(r, 12),
		}
	}
	return out
}

// textRows renders sheet cells the way freeze writes them to CSV: numbers with
// a dot decimal separator and no exponent, everything else as printed.
func textRows(rows [][]interface{}) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		rec := make([]string, len(r))
		for j, c := range r {
			if v, ok := c.(float64); ok {
				rec[j] = strconv.FormatFloat(v, 'f', -1, 64)
			} else {
				rec[j] = fmt.Sprint(c)
			}
		}
		out[i] = rec
	}
	return out
}

// text returns a column trimmed, or "" when the row is short.
func text(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// textNumber returns a column as a float, or 0 when it is blank or not numeric.
func textNumber(row []string, i int) float64 {
	f, _ := strconv.ParseFloat(text(row, i), 64)
	return f
}

// textOptNumber is textNumber for a column that is legitimately blank: it
// returns nil rather than 0, so a cell nobody has filled in cannot pass for a
// score of 0.
func textOptNumber(row []string, i int) *float64 {
	f, err := strconv.ParseFloat(text(row, i), 64)
	if err != nil {
		return nil
	}
	return &f
}
