package main

import (
	"strings"
	"testing"
)

func someCandidates(n int) []pubCandidate {
	var out []pubCandidate
	for i := 1; i <= n; i++ {
		out = append(out, pubCandidate{Operator: testOperator(i), Moniker: "V", Total: 5000, Rank: i, Arenas: []int64{5000}})
	}
	return out
}

var shaA, shaB = strings.Repeat("a", 64), strings.Repeat("b", 64)

var (
	metaA = editionMeta{ComputedAt: "2026-10-13T00:00:00Z", SHA256: shaA, Arenas: []string{"challenge"}}
	metaB = editionMeta{ComputedAt: "2026-10-13T00:00:00Z", SHA256: shaB, Arenas: []string{"challenge"}}
)

// sealedOf is the LatestJSON of an edition holding exactly cands.
func sealedOf(number int, meta editionMeta, cands []pubCandidate) *editionJSON {
	e := &editionJSON{Number: number, ComputedAt: meta.ComputedAt, ScorecardSHA256: meta.SHA256, Arenas: meta.Arenas, Candidates: []candidateJSON{}}
	for _, c := range cands {
		e.Candidates = append(e.Candidates, candidateJSON{Operator: c.Operator, Moniker: c.Moniker, Total: c.Total, Rank: c.Rank, Github: c.Github, ArenaScores: c.Arenas})
	}
	return e
}

func TestPlanPublishOpensWhenChainIsEmpty(t *testing.T) {
	step, err := planPublish(nil, nil, metaA, someCandidates(3))
	if err != nil || step.Kind != stepOpen || len(step.Missing) != 3 {
		t.Fatalf("step %+v, err %v", step, err)
	}
}

func TestPlanPublishResumesSameScorecard(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaA, Operators: []string{testOperator(2)}}
	step, err := planPublish(open, nil, metaA, someCandidates(3))
	if err != nil || step.Kind != stepResume || step.Edition != 2 || len(step.Missing) != 2 {
		t.Fatalf("step %+v, err %v", step, err)
	}
	for _, c := range step.Missing {
		if c.Operator == testOperator(2) {
			t.Fatal("a candidate already on chain is sent again")
		}
	}
	open.Operators = []string{testOperator(1), testOperator(2), testOperator(3)}
	step, err = planPublish(open, nil, metaA, someCandidates(3))
	if err != nil || step.Kind != stepResume || len(step.Missing) != 0 {
		t.Fatalf("complete open edition: step %+v, err %v", step, err)
	}
}

func TestPlanPublishRefusesOtherOpenScorecard(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaB}
	_, err := planPublish(open, nil, metaA, someCandidates(3))
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") || !strings.Contains(err.Error(), shortSHA(shaB)) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanPublishRefusesForeignOperatorInOpenEdition(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaA, Operators: []string{testOperator(9)}}
	_, err := planPublish(open, nil, metaA, someCandidates(3))
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanPublishNothingWhenAlreadySealed(t *testing.T) {
	step, err := planPublish(nil, sealedOf(1, metaA, someCandidates(3)), metaA, someCandidates(3))
	if err != nil || step.Kind != stepNothing || step.Edition != 1 {
		t.Fatalf("step %+v, err %v", step, err)
	}
}

// Same scorecard and same operators, but the sealed lines differ (here two
// operators swapped between candidates, as a valoper-map fix would do): the
// run is not "already published"; it opens a new edition and says why.
func TestPlanPublishComparesEverySealedLine(t *testing.T) {
	cands := someCandidates(3)
	sealed := someCandidates(3)
	sealed[0].Operator, sealed[1].Operator = sealed[1].Operator, sealed[0].Operator
	sealed[0].Moniker, sealed[1].Moniker = "A", "B"
	cands[0].Moniker, cands[1].Moniker = "B", "A"
	step, err := planPublish(nil, sealedOf(1, metaA, sealed), metaA, cands)
	if err != nil || step.Kind != stepOpen || step.Supersedes != 1 || len(step.Missing) != 3 {
		t.Fatalf("step %+v, err %v", step, err)
	}
	if got := step.describe(3); !strings.Contains(got, "sealed edition 1 holds this scorecard with other lines") {
		t.Fatalf("describe = %q", got)
	}
	other := metaA
	other.Arenas = []string{"pearl"}
	if step, err := planPublish(nil, sealedOf(1, other, cands), metaA, cands); err != nil || step.Kind != stepOpen {
		t.Fatalf("other arenas: step %+v, err %v", step, err)
	}
}

func TestPlanPublishOpensWhenSealedEditionDiffers(t *testing.T) {
	latest := sealedOf(1, metaA, someCandidates(1))
	step, err := planPublish(nil, latest, metaA, someCandidates(2)) // a new map entry matched one more
	if err != nil || step.Kind != stepOpen || len(step.Missing) != 2 {
		t.Fatalf("same scorecard, more candidates: step %+v, err %v", step, err)
	}
	step, err = planPublish(nil, latest, metaB, someCandidates(1)) // a corrected scorecard
	if err != nil || step.Kind != stepOpen {
		t.Fatalf("new scorecard: step %+v, err %v", step, err)
	}
}

func TestPublishStepDescribe(t *testing.T) {
	for _, c := range []struct {
		step publishStep
		want string
	}{
		{publishStep{Kind: stepOpen, Missing: someCandidates(3)}, "open a new edition, add 3 candidate(s), seal with 3"},
		{publishStep{Kind: stepResume, Edition: 2, Missing: someCandidates(1)}, "resume open edition 2: add 1 missing candidate(s), seal with 3"},
		{publishStep{Kind: stepNothing, Edition: 1}, "already published as edition 1 (LatestJSON matches this run): nothing to send"},
	} {
		if got := c.step.describe(3); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
