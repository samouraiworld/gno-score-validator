package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/firedrill"
)

// firedrillHeader is the tab and the CSV, which hold the same nine columns.
// Bilan reads only column C, by moniker.
var firedrillHeader = []string{
	"moniker", "portal_moniker", "score",
	"upload_time_score", "metadata_score", "log_quality_score",
	"incident_response_quality_score", "findings", "operator_address",
}

// runFiredrill rebuilds the fire drill arena CSV from the portal exports. It
// writes the CSV only, like fetch: run score, then sheet push, afterwards.
//
// It merges rather than regenerates. The findings column carries hand-written
// conduct notes — "Walked another validator through the whole signing chain" —
// that justify the manual incident-response mark and exist in no export. Rows
// already present keep their findings and their moniker spelling verbatim;
// only genuinely new submitters get a generated, automated-checks-only line.
func runFiredrill(e *env, args []string) error {
	fs := flag.NewFlagSet("firedrill", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	scoresPath := fs.String("scores", "scores.json", "portal scores export")
	subsPath := fs.String("submissions", "submissions.jsonl", "portal submissions export")
	csvPath := fs.String("csv", "", "rubric CSV (default the fire drill arena's CSV under data/arenas/)")
	force := fs.Bool("force", false, "rewrite the frozen fire drill result")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := e.arenas()
	if err != nil {
		return err
	}
	var drill *arena.Arena
	for i, a := range cfg.Arenas {
		if a.Detail == arena.DetailFireDrill {
			drill = &cfg.Arenas[i]
		}
	}
	if drill == nil {
		return fmt.Errorf("config/arenas.json has no arena with detail %q", arena.DetailFireDrill)
	}
	if !*dry {
		if err := guardFrozen(drill.Tab, *force); err != nil {
			return err
		}
	}
	if *csvPath == "" {
		*csvPath = drill.File(e.data(""))
	}

	results, err := firedrill.Load(*scoresPath, *subsPath)
	if err != nil {
		return err
	}
	prevByPortal, prevScore, err := loadFiredrillCSV(*csvPath)
	if err != nil {
		return err
	}
	known, err := e.scorecardMonikers()
	if err != nil {
		return err
	}

	type row struct {
		moniker string
		cells   []interface{}
	}
	var rows []row
	var added, changed int

	for _, r := range results {
		prev, seen := prevByPortal[r.Moniker]

		moniker := prev.moniker
		findings := prev.findings
		if !seen {
			// Resolve the portal's spelling against the scorecard, which is
			// the only name Bilan's VLOOKUP will match.
			if m, ok := known[strings.ToLower(r.Moniker)]; ok {
				moniker = m
			} else {
				moniker = r.Moniker
				fmt.Printf("  ! %-26s no scorecard moniker matches; scores nothing until mapped\n", r.Moniker)
			}
			findings = r.AutoFindings()
			added++
			fmt.Printf("  + %-26s %3d  %s\n", moniker, r.Total, findings)
		} else if old, ok := prevScore[r.Moniker]; ok && old != r.Total {
			changed++
			fmt.Printf("  ~ %-26s %3d -> %-3d\n", moniker, old, r.Total)
		}

		rows = append(rows, row{moniker, []interface{}{
			moniker, r.Moniker, r.Total,
			r.UploadTime, r.Metadata, r.LogQuality, r.IncidentAdjusted(),
			findings, r.Address,
		}})
	}

	for portal := range prevByPortal {
		if !hasPortal(results, portal) {
			fmt.Printf("  - %-26s was in the CSV but not in the exports; dropped\n", portal)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].moniker) < strings.ToLower(rows[j].moniker)
	})

	fmt.Printf("\n%d submitters (%d new, %d rescored), %d candidates score 0\n",
		len(rows), added, changed, len(known)-len(rows))
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}

	out := [][]interface{}{toIface(firedrillHeader)}
	for _, r := range rows {
		out = append(out, r.cells)
	}
	if err := writeFiredrillCSV(*csvPath, out); err != nil {
		return err
	}

	fmt.Printf("wrote %s; run score, then sheet push.\n", *csvPath)
	return nil
}

type firedrillPrev struct{ moniker, findings string }

// loadFiredrillCSV returns the hand-curated half of each existing row, keyed by
// the portal's spelling, plus the score it recorded.
func loadFiredrillCSV(path string) (map[string]firedrillPrev, map[string]int, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]firedrillPrev{}, map[string]int{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	prev := map[string]firedrillPrev{}
	score := map[string]int{}
	for _, r := range recs[1:] {
		if len(r) < 9 {
			continue
		}
		prev[r[1]] = firedrillPrev{moniker: r[0], findings: r[7]}
		if n, err := strconv.Atoi(r[2]); err == nil {
			score[r[1]] = n
		}
	}
	return prev, score, nil
}

func writeFiredrillCSV(path string, rows [][]interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, c := range r {
			rec[i] = fmt.Sprint(c)
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// scorecardMonikers indexes the candidate list by lowercased moniker, the way
// the sheet's VLOOKUP matches it.
func (e *env) scorecardMonikers() (map[string]string, error) {
	b, err := os.ReadFile(e.data("scorecard.json"))
	if err != nil {
		return nil, err
	}
	var s struct {
		Validators []struct {
			Moniker string `json:"moniker"`
		} `json:"validators"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(s.Validators))
	for _, v := range s.Validators {
		out[strings.ToLower(v.Moniker)] = v.Moniker
	}
	return out, nil
}

func hasPortal(rs []firedrill.Result, portal string) bool {
	for _, r := range rs {
		if r.Moniker == portal {
			return true
		}
	}
	return false
}

func toIface(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
