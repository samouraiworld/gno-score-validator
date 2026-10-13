package scoring

import (
	"fmt"
	"sort"
)

// ArenaScores is one arena's input to the scorecard: its scores keyed by
// Key(moniker), and whether they count toward the total.
type ArenaScores struct {
	ID      string
	InTotal bool
	Scores  map[string]float64
}

// Inputs is everything a total is made of. Every map is keyed by Key(moniker).
type Inputs struct {
	Monikers []string // the candidates, in display order
	Arenas   []ArenaScores

	// GitHub participation from CutoffYear on, the score the totals use.
	Github map[string]float64
	// The same participation counted all-time, for the validators with an
	// evidence block. A candidate missing here has no cutoff to undo: its
	// uncorrected score is its corrected one.
	GithubAllTime map[string]float64
}

// Result is one candidate's scores. The corrected pair applies CutoffYear to
// GitHub; the current pair does not, and only differs where the cutoff bites.
type Result struct {
	Moniker         string
	Components      map[string]float64 // arena id -> score, 0 when absent from the arena
	GithubCurrent   float64
	GithubCorrected float64
	ScoreCurrent    float64
	ScoreCorrected  float64
	RankCurrent     int
	RankCorrected   int
}

// Compute scores every candidate, in the order given. A moniker absent from an
// arena scores 0 there: there is no last-known-value fallback.
func Compute(in Inputs) ([]Result, error) {
	divisor := 0
	for _, a := range in.Arenas {
		if a.InTotal {
			divisor++
		}
	}
	if divisor == 0 {
		return nil, fmt.Errorf("no arena counts toward the total")
	}

	out := make([]Result, 0, len(in.Monikers))
	seen := map[string]bool{}
	for _, moniker := range in.Monikers {
		k := Key(moniker)
		if k == "" {
			return nil, fmt.Errorf("blank moniker")
		}
		if seen[k] {
			return nil, fmt.Errorf("moniker %q appears twice", moniker)
		}
		seen[k] = true

		r := Result{Moniker: moniker, Components: map[string]float64{}}
		counted := make([]float64, 0, divisor)
		for _, a := range in.Arenas {
			s := a.Scores[k]
			r.Components[a.ID] = s
			if a.InTotal {
				counted = append(counted, s)
			}
		}
		r.GithubCorrected = in.Github[k]
		r.GithubCurrent = r.GithubCorrected
		if g, ok := in.GithubAllTime[k]; ok {
			r.GithubCurrent = g
		}
		r.ScoreCorrected = Total(counted, r.GithubCorrected)
		r.ScoreCurrent = Total(counted, r.GithubCurrent)
		out = append(out, r)
	}

	current := make([]float64, len(out))
	corrected := make([]float64, len(out))
	for i, r := range out {
		current[i], corrected[i] = r.ScoreCurrent, r.ScoreCorrected
	}
	rc, rk := Rank(current), Rank(corrected)
	for i := range out {
		out[i].RankCurrent, out[i].RankCorrected = rc[i], rk[i]
	}
	return out, nil
}

// Total is the scoring rule: the mean of the arena scores that count, plus the
// GitHub bonus, capped at MaxScore. The arenas are summed in the order given,
// so the result is bit-for-bit the one the spreadsheet formula gives.
func Total(arenas []float64, github float64) float64 {
	sum := 0.0
	for _, s := range arenas {
		sum += s
	}
	t := sum/float64(len(arenas)) + github/BonusDivisor
	if t > MaxScore {
		return MaxScore
	}
	return t
}

// GithubScore applies the participation weights to a contribution breakdown.
func GithubScore(c map[string]float64) float64 {
	return WeightMergedPR*c["merged_prs"] +
		WeightUnmergedPR*c["unmerged_prs"] +
		WeightIssue*c["issues"] +
		WeightCommit*c["commits"] +
		WeightComment*c["comment_threads"]
}

// Rank returns the competition rank of each score, highest first: equal
// scores share a rank and the next one skips (1, 2, 2, 4). Ten candidates
// share a score, and list order is no reason to put one above another.
func Rank(scores []float64) []int {
	idx := make([]int, len(scores))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	ranks := make([]int, len(scores))
	shared := 0
	for r, i := range idx {
		if r == 0 || abs(scores[i]-scores[idx[r-1]]) > 1e-9 {
			shared = r + 1
		}
		ranks[i] = shared
	}
	return ranks
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
