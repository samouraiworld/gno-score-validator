package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// Mainnet gnoland-1 is shown beside the scorecard, not inside it. On
// 2026-10-12 its six validators were all team nodes and none of the 98
// candidates, so a seventh equal-weight arena would only have scaled every
// published score down by a seventh. Its weight is to be decided when a
// candidate first joins it. Until then no total reads this block.
//
// `fetch -chain gnoland-1` writes the report to data/arenas/mainnet.csv, and
// score and snapshot both build the block from that file, so it is derived
// like everything else in scorecard.json.

// snapMainnet is one fetched mainnet report.
type snapMainnet struct {
	Chain      string           `json:"chain"`
	Period     string           `json:"period"`
	FetchedAt  string           `json:"fetched_at"`
	Validators []snapMainnetRow `json:"validators"`
}

// snapMainnetRow carries the same metrics as a testnet arena, plus whether the
// validator is one of the scorecard's candidates.
type snapMainnetRow struct {
	Moniker   string `json:"moniker"`
	Candidate bool   `json:"candidate"`
	snapOnchain
}

// loadMainnet builds the mainnet block from the config's mainnet entry and its
// CSV, ordered by score then moniker so a refetch diffs cleanly. vs are the
// scorecard's validators, which say who is a candidate. No mainnet entry in
// config means no block.
func loadMainnet(cfg arena.Config, dataDir string, vs []snapValidator) (*snapMainnet, error) {
	a := cfg.Mainnet
	if a == nil {
		return nil, nil
	}
	header, rows, err := a.Table(dataDir)
	if err != nil {
		return nil, err
	}
	period, err := fetchPeriod(*a)
	if err != nil {
		return nil, err
	}
	path := a.File(dataDir)
	// parseOnchain reads by position, so the layout must be fetch's own.
	if len(header) < len(fetchHeader(period)) || header[0] != a.MonikerColumn || header[2] != a.ScoreColumn {
		return nil, fmt.Errorf("%s: header %v is not the layout fetch writes", path, header)
	}
	fi := arena.Column(header, fetchedAtColumn)
	if fi < 0 {
		return nil, fmt.Errorf("%s: no %s column", path, fetchedAtColumn)
	}

	candidate := map[string]bool{}
	for _, v := range vs {
		candidate[scoring.Key(v.Moniker)] = true
	}
	m := &snapMainnet{Chain: a.Chain, Period: period}
	seen := map[string]bool{}
	for n, r := range rows {
		moniker := text(r, 0)
		k := scoring.Key(moniker)
		if k == "" {
			continue
		}
		if seen[k] {
			return nil, fmt.Errorf("%s line %d: %s is listed twice", path, n+2, moniker)
		}
		seen[k] = true
		// One fetch, one date: a file stitched from two fetches would show
		// a date only some of its rows are true for.
		switch at := text(r, fi); {
		case at == "":
			return nil, fmt.Errorf("%s line %d: %s has no %s", path, n+2, moniker, fetchedAtColumn)
		case m.FetchedAt == "":
			m.FetchedAt = at
		case at != m.FetchedAt:
			return nil, fmt.Errorf("%s line %d: fetched %s, earlier rows %s", path, n+2, at, m.FetchedAt)
		}
		m.Validators = append(m.Validators, snapMainnetRow{
			Moniker: moniker, Candidate: candidate[k], snapOnchain: onchainRow(r),
		})
	}
	if len(m.Validators) == 0 {
		return nil, fmt.Errorf("%s: no validators", path)
	}
	sort.SliceStable(m.Validators, func(i, j int) bool {
		x, y := m.Validators[i], m.Validators[j]
		if x.Score != y.Score {
			return x.Score > y.Score
		}
		return scoring.Key(x.Moniker) < scoring.Key(y.Moniker)
	})
	return m, nil
}

// saveSnapshot writes the snapshot in the indented form the repo commits.
func saveSnapshot(path string, s snapshot) error {
	pretty, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(pretty, '\n'), 0o644)
}
