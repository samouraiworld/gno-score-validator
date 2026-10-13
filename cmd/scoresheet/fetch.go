package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/monitoring"
	"github.com/louisb/score-validator/internal/scoring"
)

// fetchedAtColumn dates the report a fetched CSV holds. Every row carries the
// same date, so the file alone says when it was true.
const fetchedAtColumn = "fetched_at"

// runFetch writes the live gnomonitoring report for a chain to the CSV of the
// config arena that names that chain. It is refresh without the sheet: no -id,
// no key, and the same guards — a frozen arena is refused without -force, and
// so is a report that loses more than a fifth of the validators already in
// the file.
//
// It writes the CSV only. Run score afterwards to rebuild scorecard.json.
func runFetch(root string, args []string) error {
	fset := flag.NewFlagSet("fetch", flag.ExitOnError)
	dry := fset.Bool("dry", false, "print the plan without writing")
	chain := fset.String("chain", "", "gnomonitoring chain id (required)")
	force := fset.Bool("force", false, "rewrite a frozen arena, or accept a report that shrank")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if *chain == "" {
		return fmt.Errorf("fetch needs -chain")
	}
	cfg, err := arena.LoadConfig(filepath.Join(root, "config", "arenas.json"))
	if err != nil {
		return err
	}
	// The chain picks the file through config, never a default: a default
	// target once let refresh write pearl data over Score-sapphire.
	a, ok := cfg.ByChain(*chain)
	if !ok {
		return fmt.Errorf("no arena in config/arenas.json has chain %q", *chain)
	}
	period, err := fetchPeriod(a)
	if err != nil {
		return err
	}
	if !*dry {
		if err := guardFrozen(a.Tab, *force); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	report, err := monitoring.Fetch(ctx, *chain)
	if err != nil {
		return err
	}
	data := filepath.Join(root, "data")
	path := a.File(data)
	rows, err := fetchRows(report, period, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("%s: %w, refusing to touch %s", *chain, err, path)
	}

	current, err := arena.LoadScores(data, a)
	if errors.Is(err, fs.ErrNotExist) {
		current, err = map[string]float64{}, nil
	}
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var added, changed int
	for _, r := range rows[1:] {
		k := scoring.Key(r[0])
		seen[k] = true
		score, _ := strconv.ParseFloat(r[2], 64)
		old, known := current[k]
		switch {
		case !known:
			added++
			fmt.Printf("  + %-26s %-12s score %g\n", r[0], "(new)", score)
		case old != score:
			changed++
			fmt.Printf("  ~ %-26s %6g -> %-6g\n", r[0], old, score)
		}
	}
	var dropped []string
	for k := range current {
		if !seen[k] {
			dropped = append(dropped, k)
		}
	}
	sort.Strings(dropped)
	for _, k := range dropped {
		fmt.Printf("  - %-26s %6g -> gone from the report, scores 0 there\n", k, current[k])
	}
	fmt.Printf("\n%s <- %s/%s: %d rows (%d new, %d changed, %d dropped)\n",
		path, *chain, period, len(rows)-1, added, changed, len(dropped))
	if !a.InTotal {
		fmt.Println("shown, not scored: no total reads this arena")
	}

	// topaz-1 once answered with 2 validators where the tab held 85: a report
	// that loses a fifth of the file is an outage or a retired chain, not news.
	shrank := len(dropped)*5 > len(current)
	if *dry {
		if shrank {
			fmt.Printf("%d of %d validators would drop out: a real run needs -force\n", len(dropped), len(current))
		}
		if scoring.IsFrozen(a.Tab) {
			fmt.Printf("%s is frozen: a real run needs -force\n", a.ID)
		}
		fmt.Println("-dry — nothing written.")
		return nil
	}
	if shrank && !*force {
		return fmt.Errorf("%d of %d validators would drop out of %s; rerun with -force if that is real",
			len(dropped), len(current), path)
	}
	if err := writeCSV(path, rows); err != nil {
		return err
	}
	fmt.Printf("wrote %s. Run score to rebuild data/scorecard.json.\n", path)
	return nil
}

// fetchPeriod is the report period an arena's CSV holds, read from its score
// column: fetch writes "<period>_score", and the config must name that column
// or score could not read the file back.
func fetchPeriod(a arena.Arena) (string, error) {
	period, ok := strings.CutSuffix(a.ScoreColumn, "_score")
	switch {
	case a.MonikerColumn != "moniker":
		return "", fmt.Errorf("arena %q: fetch writes moniker_column \"moniker\", config says %q", a.ID, a.MonikerColumn)
	case !ok:
		return "", fmt.Errorf("arena %q: score_column %q is not <period>_score", a.ID, a.ScoreColumn)
	}
	switch period {
	case monitoring.PeriodLast24h, monitoring.PeriodWeek, monitoring.PeriodMonth, monitoring.PeriodYear:
		return period, nil
	}
	return "", fmt.Errorf("arena %q: %q is not a gnomonitoring period", a.ID, period)
}

// fetchHeader is the layout of a fetched CSV: the network tab layout refresh
// writes, which parseOnchain reads by position, then the fetch date.
func fetchHeader(period string) []string {
	h := []string{"moniker", "address"}
	for _, c := range onchainColumns {
		h = append(h, period+"_"+c)
	}
	return append(h, fetchedAtColumn)
}

// fetchRows turns a report into CSV rows, header first, sorted by moniker so
// a refetch diffs cleanly. An empty report is an outage and is refused, so is
// a moniker listed twice under any case, since the join is by moniker.
func fetchRows(report []monitoring.Validator, period, fetchedAt string) ([][]string, error) {
	if len(report) == 0 {
		return nil, fmt.Errorf("empty report")
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	rows := [][]string{fetchHeader(period)}
	seen := map[string]bool{}
	for _, v := range report {
		k := scoring.Key(v.Moniker)
		if seen[k] {
			return nil, fmt.Errorf("moniker %q listed twice", v.Moniker)
		}
		seen[k] = true
		m, ok := v.Periods[period]
		if !ok {
			return nil, fmt.Errorf("%s has no period %q", v.Moniker, period)
		}
		// A null proposer reliability means the validator was never asked to
		// propose. Writing 0 would read as a failure to do so.
		proposer := ""
		if m.Proposer != nil {
			proposer = num(*m.Proposer)
		}
		rows = append(rows, []string{
			v.Moniker, v.Address,
			num(m.Score), m.Tier, num(m.SignRate), num(m.VotingPower), proposer,
			num(m.Critical), num(m.Warning), num(m.Incidents), num(m.IncidentRate),
			num(m.Downtime), num(m.Missed), fetchedAt,
		})
	}
	body := rows[1:]
	sort.SliceStable(body, func(i, j int) bool { return scoring.Key(body[i][0]) < scoring.Key(body[j][0]) })
	return rows, nil
}

// writeCSV writes rows to path through a temporary file, so a failed write
// leaves the previous report in place.
func writeCSV(path string, rows [][]string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// guardFrozen refuses to rewrite a final arena result unless forced.
func guardFrozen(tab string, force bool) error {
	if scoring.IsFrozen(tab) && !force {
		return fmt.Errorf("%s holds a final arena result and is frozen; rerun with -force only to correct it", tab)
	}
	return nil
}

// onchainColumns name the metrics of an onchain arena CSV in file order,
// after moniker and address. The header is written from these, prefixed by
// the period, so a file can never claim to hold a window it does not.
var onchainColumns = []string{
	"score", "tier", "sign", "vp", "proposer",
	"critical", "warning", "freq", "freq_rate", "downtime", "missed",
}
