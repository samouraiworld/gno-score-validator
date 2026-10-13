package scoring

import "testing"

func TestComputeJoinsByKeyAndScoresAbsenceAsZero(t *testing.T) {
	in := Inputs{
		Monikers: []string{"Alpha", "beta", "Gamma"},
		Arenas: []ArenaScores{
			{ID: "a", InTotal: true, Scores: map[string]float64{"alpha": 60, "beta": 90}},
			{ID: "b", InTotal: true, Scores: map[string]float64{"alpha": 60, "gamma": 100}},
			{ID: "shown", InTotal: false, Scores: map[string]float64{"alpha": 100}},
		},
		Github:        map[string]float64{"alpha": 50, "gamma": 200},
		GithubAllTime: map[string]float64{"gamma": 400},
	}
	got, err := Compute(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		// (60+60)/2 + 50/10; the arena not in the total is shown, never summed.
		{Moniker: "Alpha", Components: map[string]float64{"a": 60, "b": 60, "shown": 100},
			GithubCurrent: 50, GithubCorrected: 50, ScoreCurrent: 65, ScoreCorrected: 65, RankCurrent: 2, RankCorrected: 2},
		// Absent from arena b: 0 there, no fallback.
		{Moniker: "beta", Components: map[string]float64{"a": 90, "b": 0, "shown": 0},
			ScoreCurrent: 45, ScoreCorrected: 45, RankCurrent: 3, RankCorrected: 3},
		// (0+100)/2 + 200/10 = 70 corrected, uncapped all-time pushes 90.
		{Moniker: "Gamma", Components: map[string]float64{"a": 0, "b": 100, "shown": 0},
			GithubCurrent: 400, GithubCorrected: 200, ScoreCurrent: 90, ScoreCorrected: 70, RankCurrent: 1, RankCorrected: 1},
	}
	for i, w := range want {
		g := got[i]
		if g.Moniker != w.Moniker || g.GithubCurrent != w.GithubCurrent || g.GithubCorrected != w.GithubCorrected ||
			g.ScoreCurrent != w.ScoreCurrent || g.ScoreCorrected != w.ScoreCorrected ||
			g.RankCurrent != w.RankCurrent || g.RankCorrected != w.RankCorrected {
			t.Errorf("%s: got %+v, want %+v", w.Moniker, g, w)
		}
		for id, s := range w.Components {
			if g.Components[id] != s {
				t.Errorf("%s: component %s = %g, want %g", w.Moniker, id, g.Components[id], s)
			}
		}
	}
}

func TestComputeRejects(t *testing.T) {
	arenas := []ArenaScores{{ID: "a", InTotal: true}}
	for name, in := range map[string]Inputs{
		"duplicate moniker": {Monikers: []string{"x", " X "}, Arenas: arenas},
		"blank moniker":     {Monikers: []string{" "}, Arenas: arenas},
		"nothing in total":  {Monikers: []string{"x"}, Arenas: []ArenaScores{{ID: "a"}}},
	} {
		if _, err := Compute(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTotalCaps(t *testing.T) {
	if got := Total([]float64{100, 100}, 100); got != MaxScore {
		t.Errorf("Total = %g, want the cap of %d", got, MaxScore)
	}
}

func TestRankSharesTies(t *testing.T) {
	got := Rank([]float64{70, 90, 70, 50})
	want := []int{2, 1, 2, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Rank = %v, want %v", got, want)
			break
		}
	}
}
