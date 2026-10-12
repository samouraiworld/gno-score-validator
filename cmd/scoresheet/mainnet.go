package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/louisb/score-validator/internal/monitoring"
	"github.com/louisb/score-validator/internal/scoring"
)

// Mainnet gnoland-1 is shown beside the scorecard, not inside it. On
// 2026-10-12 its six validators were all team nodes and none of the 98
// candidates, so a seventh equal-weight arena would only have scaled every
// published score down by a seventh. Its weight is to be decided when a
// candidate first joins it. Until then no total reads this block.
//
// The report is fetched straight into data/scorecard.json: the sheet holds no
// mainnet tab, so snapshot carries the block across untouched, like the
// GitHub evidence.

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

func runMainnet(root string, args []string) error {
	fs := flag.NewFlagSet("mainnet", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the report without writing")
	force := fs.Bool("force", false, "accept a report that loses more than a fifth of the validators")
	chain := fs.String("chain", "gnoland-1", "gnomonitoring chain id")
	period := fs.String("period", monitoring.PeriodYear, "report period, as for the testnet arenas")
	in := fs.String("in", "", "snapshot file (default data/scorecard.json)")
	page := fs.String("html", "", "page to re-embed the public payload in (default web/validator-scorecard.html)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		*in = filepath.Join(root, "data", "scorecard.json")
	}
	if *page == "" {
		*page = filepath.Join(root, "web", "validator-scorecard.html")
	}

	s, err := loadSnapshot(*in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	report, err := monitoring.Fetch(ctx, *chain)
	if err != nil {
		return err
	}
	m, err := mainnetFromReport(report, *chain, *period, s.Validators)
	if err != nil {
		return err
	}
	m.FetchedAt = time.Now().UTC().Format("2006-01-02")

	candidates := 0
	for _, r := range m.Validators {
		mark := ""
		if r.Candidate {
			candidates++
			mark = "  candidate"
		}
		fmt.Printf("  %-28s %-10s score %g%s\n", r.Moniker, r.Tier, r.Score, mark)
	}
	fmt.Printf("\n%s/%s: %d validators, %d of them candidates — not part of any total\n",
		*chain, *period, len(m.Validators), candidates)

	if s.Mainnet != nil && s.Mainnet.Chain == *chain {
		seen := map[string]bool{}
		for _, r := range m.Validators {
			seen[scoring.Key(r.Moniker)] = true
		}
		dropped := 0
		for _, r := range s.Mainnet.Validators {
			if !seen[scoring.Key(r.Moniker)] {
				dropped++
				fmt.Printf("  - %s gone from the report\n", r.Moniker)
			}
		}
		// Same rule as refresh: losing a fifth of the set is an outage, not news.
		if dropped*5 > len(s.Mainnet.Validators) && !*force && !*dry {
			return fmt.Errorf("%d of %d mainnet validators would drop out; rerun with -force if that is real",
				dropped, len(s.Mainnet.Validators))
		}
	}
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}

	s.Mainnet = m
	if err := saveSnapshot(*in, s); err != nil {
		return err
	}
	if err := embedPage(s, *page); err != nil {
		return err
	}
	fmt.Printf("wrote the mainnet block to %s and re-embedded %s\n", *in, *page)
	return render(s, root)
}

// mainnetFromReport turns a gnomonitoring report into the mainnet block,
// ordered by score then moniker so a refetch diffs cleanly. An empty report
// is refused: it is an outage, and writing it would erase the last good one.
func mainnetFromReport(report []monitoring.Validator, chain, period string, vs []snapValidator) (*snapMainnet, error) {
	if len(report) == 0 {
		return nil, fmt.Errorf("%s: empty report", chain)
	}
	candidate := map[string]bool{}
	for _, v := range vs {
		candidate[scoring.Key(v.Moniker)] = true
	}
	m := &snapMainnet{Chain: chain, Period: period}
	seen := map[string]bool{}
	for _, v := range report {
		k := scoring.Key(v.Moniker)
		if seen[k] {
			return nil, fmt.Errorf("%s: moniker %q listed twice", chain, v.Moniker)
		}
		seen[k] = true
		p, ok := v.Periods[period]
		if !ok {
			return nil, fmt.Errorf("%s: %s has no period %q", chain, v.Moniker, period)
		}
		m.Validators = append(m.Validators, snapMainnetRow{
			Moniker:   v.Moniker,
			Candidate: candidate[k],
			snapOnchain: snapOnchain{
				Address: v.Address, Tier: p.Tier, Score: p.Score, Sign: p.SignRate,
				VP: p.VotingPower, Proposer: p.Proposer, Critical: p.Critical,
				Warning: p.Warning, Freq: p.Incidents, FreqRate: p.IncidentRate,
				Downtime: p.Downtime, Missed: p.Missed,
			},
		})
	}
	sort.SliceStable(m.Validators, func(a, b int) bool {
		x, y := m.Validators[a], m.Validators[b]
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
