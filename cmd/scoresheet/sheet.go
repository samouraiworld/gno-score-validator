package main

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// The spreadsheet is an output of the repo. `sheet push` writes it from
// data/scorecard.json and the arena CSVs, `sheet diff` lists every cell where
// it no longer says what the repo says, and `sheet lock` protects every tab so
// a hand edit is refused instead of silently diverging.
func runSheet(e *env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sheet needs a subcommand: diff, lock or push")
	}
	switch args[0] {
	case "diff":
		return runSheetDiff(e, args[1:])
	case "lock":
		return runSheetLock(e, args[1:])
	case "push":
		return runSheetPush(e, args[1:])
	default:
		return fmt.Errorf("unknown sheet subcommand %q: want diff, lock or push", args[0])
	}
}

// sheetTab is one tab as the repo says it should read: a header row, then one
// row per validator. Cells are string or float64. A float64 is sent as a JSON
// number, never as text, so the fr_FR locale cannot reread it.
type sheetTab struct {
	Name string
	Key  int // column holding the moniker, the only join key
	Rows [][]interface{}
}

// Bilan is generated: moniker and Discord, one column per arena in config
// order, then the GitHub bonus, the total and the GitHub audit pair. Every
// cell is a value; the total is computed in Go, not by a sheet formula.
const (
	bilanMoniker = 0
	bilanDiscord = 1
	bilanArena0  = 2
)

type bilanColumns struct {
	GitHub, Score, Login, Confidence int
}

func bilanLayout(cfg arena.Config) bilanColumns {
	n := bilanArena0 + len(cfg.Arenas)
	return bilanColumns{GitHub: n, Score: n + 1, Login: n + 2, Confidence: n + 3}
}

// expectedTabs builds every tab the repo owns: Bilan first, then one tab per
// arena of config/arenas.json. It refuses a scorecard.json that no longer
// matches the CSVs, since Bilan would then disagree with the arena tabs
// written next to it.
func expectedTabs(root string) (arena.Config, []sheetTab, error) {
	data := filepath.Join(root, "data")
	cfg, err := arena.LoadConfig(filepath.Join(root, "config", "arenas.json"))
	if err != nil {
		return cfg, nil, err
	}
	card, err := loadSnapshot(filepath.Join(data, "scorecard.json"))
	if err != nil {
		return cfg, nil, err
	}
	if err := checkScorecardFresh(root, card); err != nil {
		return cfg, nil, err
	}
	candidates, err := arena.LoadCandidates(filepath.Join(data, "candidates.csv"))
	if err != nil {
		return cfg, nil, err
	}
	links, err := scoring.LoadLinks(filepath.Join(data, "github-links.csv"))
	if err != nil {
		return cfg, nil, err
	}
	bilan, err := bilanTab(cfg, candidates, card, links)
	if err != nil {
		return cfg, nil, err
	}
	tabs := []sheetTab{bilan}
	for _, a := range cfg.Arenas {
		t, err := arenaTab(data, a)
		if err != nil {
			return cfg, nil, err
		}
		tabs = append(tabs, t)
	}
	return cfg, tabs, nil
}

// checkScorecardFresh recomputes the scorecard from the repo files and
// compares the scores with the committed file.
func checkScorecardFresh(root string, card snapshot) error {
	fresh, err := buildScorecard(root, card)
	if err != nil {
		return err
	}
	stale := func(why string) error {
		return fmt.Errorf("data/scorecard.json does not match the CSVs (%s): run score first", why)
	}
	if len(fresh.Validators) != len(card.Validators) {
		return stale(fmt.Sprintf("%d validators, the CSVs give %d", len(card.Validators), len(fresh.Validators)))
	}
	for i, f := range fresh.Validators {
		c := card.Validators[i]
		if f.Moniker != c.Moniker {
			return stale(fmt.Sprintf("row %d is %s, the CSVs give %s", i+1, c.Moniker, f.Moniker))
		}
		if len(f.Components) != len(c.Components) {
			return stale(c.Moniker + ": arena list differs")
		}
		for j, fc := range f.Components {
			if fc.ID != c.Components[j].ID || !closeEnough(fc.Score, c.Components[j].Score) {
				return stale(fmt.Sprintf("%s: %s", c.Moniker, fc.ID))
			}
		}
		if !closeEnough(f.GithubCorrected, c.GithubCorrected) || !closeEnough(f.ScoreCorrected, c.ScoreCorrected) {
			return stale(c.Moniker + ": total")
		}
	}
	return nil
}

// bilanTab lays out Bilan in candidates.csv order. Scores come from the
// scorecard, the GitHub login and confidence from github-links.csv; both are
// joined by moniker.
func bilanTab(cfg arena.Config, candidates []arena.Candidate, card snapshot, links map[string]scoring.Link) (sheetTab, error) {
	cols := bilanLayout(cfg)
	header := make([]interface{}, cols.Confidence+1)
	header[bilanMoniker], header[bilanDiscord] = "Moniker", "Discord"
	for i, a := range cfg.Arenas {
		header[bilanArena0+i] = a.Label
	}
	header[cols.GitHub], header[cols.Score] = "GitHub", "Score"
	header[cols.Login], header[cols.Confidence] = "GitHub login", "GitHub confidence"

	byKey := map[string]snapValidator{}
	for _, v := range card.Validators {
		byKey[scoring.Key(v.Moniker)] = v
	}
	rows := [][]interface{}{header}
	for _, c := range candidates {
		v, ok := byKey[scoring.Key(c.Moniker)]
		if !ok {
			return sheetTab{}, fmt.Errorf("data/scorecard.json has no %s: run score first", c.Moniker)
		}
		row := make([]interface{}, cols.Confidence+1)
		row[bilanMoniker], row[bilanDiscord] = c.Moniker, c.Discord
		for i, a := range cfg.Arenas {
			if !v.Components.Has(a.ID) {
				return sheetTab{}, fmt.Errorf("data/scorecard.json: %s has no %s score: run score first", c.Moniker, a.ID)
			}
			row[bilanArena0+i] = v.Components.Get(a.ID)
		}
		row[cols.GitHub], row[cols.Score] = v.GithubCorrected, v.ScoreCorrected
		l := links[scoring.Key(c.Moniker)]
		row[cols.Login], row[cols.Confidence] = l.Login, l.Confidence
		rows = append(rows, row)
	}
	return sheetTab{Name: scoring.TabBilan, Key: bilanMoniker, Rows: rows}, nil
}

// arenaTab is an arena's CSV as its tab: same rows, same order. A column
// whose every filled cell is a decimal number goes out as numbers; anything
// else, and the moniker column always, stays text, so a moniker such as "007"
// is never turned into 7.
func arenaTab(dataDir string, a arena.Arena) (sheetTab, error) {
	header, rows, err := a.Table(dataDir)
	if err != nil {
		return sheetTab{}, err
	}
	key := arena.Column(header, a.MonikerColumn)
	if key < 0 {
		return sheetTab{}, fmt.Errorf("%s: no column %q", a.File(dataDir), a.MonikerColumn)
	}
	numeric := numericColumns(len(header), rows, key)

	out := [][]interface{}{toIface(header)}
	for _, r := range rows {
		row := make([]interface{}, len(r))
		for j, c := range r {
			row[j] = c
			if j < len(numeric) && numeric[j] && c != "" {
				f, err := parseDecimal(c)
				if err != nil {
					return sheetTab{}, err
				}
				row[j] = f
			}
		}
		out = append(out, row)
	}
	return sheetTab{Name: a.Tab, Key: key, Rows: out}, nil
}

// decimal is the only number syntax a CSV cell may use to become a number:
// strconv.ParseFloat would also take "inf", "nan" and hex floats.
var decimal = regexp.MustCompile(`^-?(\d+\.?\d*|\.\d+)$`)

func numericColumns(width int, rows [][]string, key int) []bool {
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	numeric := make([]bool, width)
	filled := make([]bool, width)
	for j := range numeric {
		numeric[j] = j != key
	}
	for _, r := range rows {
		for j, c := range r {
			if c == "" {
				continue
			}
			filled[j] = true
			if !decimal.MatchString(c) {
				numeric[j] = false
			}
		}
	}
	for j := range numeric {
		numeric[j] = numeric[j] && filled[j]
	}
	return numeric
}

func parseDecimal(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return f, nil
}

// closeEnough compares two scores the way the sheet and Go may round them
// differently in the last bit.
func closeEnough(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(a))
}
