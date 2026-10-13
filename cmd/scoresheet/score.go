package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// componentSetters routes each arena id in config/arenas.json to its field of
// the scorecard. An arena missing here is an error rather than a score that
// silently vanishes from the page.
var componentSetters = map[string]func(*snapComponents, float64){
	"challenge": func(c *snapComponents, s float64) { c.Challenge = s },
	"test13":    func(c *snapComponents, s float64) { c.Test13 = s },
	"topaz":     func(c *snapComponents, s float64) { c.Topaz = s },
	"sapphire":  func(c *snapComponents, s float64) { c.Sapphire = s },
	"pearl":     func(c *snapComponents, s float64) { c.Pearl = s },
	"firedrill": func(c *snapComponents, s float64) { c.FireDrill = s },
}

// runScore rebuilds data/scorecard.json from the files in the repo alone —
// config/arenas.json, data/candidates.csv, data/arenas/*.csv,
// data/github-links.csv and data/github/evidence.json — then re-embeds it in
// the page and regenerates the scoreboard. No sheet, no key.
//
// Weights, source and the mainnet block are not in those files yet; they are
// carried over from the existing scorecard.json.
func runScore(root string, args []string) error {
	fs := flag.NewFlagSet("score", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	out := fs.String("out", "", "snapshot file (default data/scorecard.json)")
	page := fs.String("html", "", "page to re-embed it in (default web/validator-scorecard.html)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(root, "data", "scorecard.json")
	}
	if *page == "" {
		*page = filepath.Join(root, "web", "validator-scorecard.html")
	}

	prev, err := loadSnapshot(*out)
	if err != nil {
		return err
	}
	next, err := buildScorecard(root, prev)
	if err != nil {
		return err
	}
	next.GeneratedAt = time.Now().UTC().Format("2006-01-02")
	return publish(prev, next, *out, *page, *dry, root)
}

// buildScorecard computes the scorecard from the repo files, carrying over
// from prev only what those files do not hold. It leaves GeneratedAt as prev
// has it.
func buildScorecard(root string, prev snapshot) (snapshot, error) {
	data := filepath.Join(root, "data")
	cfg, err := arena.LoadConfig(filepath.Join(root, "config", "arenas.json"))
	if err != nil {
		return snapshot{}, err
	}
	candidates, err := arena.LoadCandidates(filepath.Join(data, "candidates.csv"))
	if err != nil {
		return snapshot{}, err
	}
	links, err := scoring.LoadLinks(filepath.Join(data, "github-links.csv"))
	if err != nil {
		return snapshot{}, err
	}
	evidence, err := loadEvidence(filepath.Join(data, evidenceFile))
	if err != nil {
		return snapshot{}, err
	}
	det, err := loadDetails(cfg, func(a arena.Arena) ([][]string, error) { return a.Rows(data) })
	if err != nil {
		return snapshot{}, err
	}

	in := scoring.Inputs{
		Github:        map[string]float64{},
		GithubAllTime: map[string]float64{},
	}
	for _, a := range cfg.Arenas {
		if _, ok := componentSetters[a.ID]; !ok {
			return snapshot{}, fmt.Errorf("arena %q has no scorecard component", a.ID)
		}
		scores, err := arena.LoadScores(data, a)
		if err != nil {
			return snapshot{}, err
		}
		in.Arenas = append(in.Arenas, scoring.ArenaScores{ID: a.ID, InTotal: a.InTotal, Scores: scores})
	}
	inCandidates := map[string]bool{}
	for _, c := range candidates {
		in.Monikers = append(in.Monikers, c.Moniker)
		inCandidates[scoring.Key(c.Moniker)] = true
	}
	for k, l := range links {
		in.Github[k] = l.Score
	}
	for k, block := range evidence {
		// A block whose moniker left the candidate list, or was respelt
		// beyond a case change, would vanish from the page without a word.
		if !inCandidates[k] {
			return snapshot{}, fmt.Errorf("%s: %q matches no candidate", evidenceFile, k)
		}
		var g struct {
			AllTime map[string]float64 `json:"all_time"`
		}
		if err := json.Unmarshal(block, &g); err != nil {
			return snapshot{}, fmt.Errorf("%s: %s: %w", evidenceFile, k, err)
		}
		in.GithubAllTime[k] = scoring.GithubScore(g.AllTime)
	}

	results, err := scoring.Compute(in)
	if err != nil {
		return snapshot{}, err
	}

	next := snapshot{
		GeneratedAt:   prev.GeneratedAt,
		Source:        prev.Source,
		Weights:       prev.Weights,
		GithubWeights: prev.GithubWeights,
		Mainnet:       prev.Mainnet,
	}
	for i, r := range results {
		v := snapValidator{
			Moniker:         r.Moniker,
			Discord:         candidates[i].Discord,
			GithubCurrent:   r.GithubCurrent,
			GithubCorrected: r.GithubCorrected,
			ScoreCurrent:    r.ScoreCurrent,
			ScoreCorrected:  r.ScoreCorrected,
			Onchain:         map[string]snapOnchain{},
			Github:          evidence[scoring.Key(r.Moniker)],
			RankCurrent:     r.RankCurrent,
			RankCorrected:   r.RankCorrected,
		}
		for id, s := range r.Components {
			componentSetters[id](&v.Components, s)
		}
		det.attach(&v)
		next.Validators = append(next.Validators, v)
	}
	return next, nil
}
