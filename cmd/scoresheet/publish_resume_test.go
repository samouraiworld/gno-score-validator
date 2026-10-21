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

func TestPlanPublishOpensWhenChainIsEmpty(t *testing.T) {
	step, err := planPublish(nil, nil, shaA, someCandidates(3))
	if err != nil || step.Kind != stepOpen || len(step.Missing) != 3 {
		t.Fatalf("step %+v, err %v", step, err)
	}
}

func TestPlanPublishResumesSameScorecard(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaA, Operators: []string{testOperator(2)}}
	step, err := planPublish(open, nil, shaA, someCandidates(3))
	if err != nil || step.Kind != stepResume || step.Edition != 2 || len(step.Missing) != 2 {
		t.Fatalf("step %+v, err %v", step, err)
	}
	for _, c := range step.Missing {
		if c.Operator == testOperator(2) {
			t.Fatal("a candidate already on chain is sent again")
		}
	}
	open.Operators = []string{testOperator(1), testOperator(2), testOperator(3)}
	step, err = planPublish(open, nil, shaA, someCandidates(3))
	if err != nil || step.Kind != stepResume || len(step.Missing) != 0 {
		t.Fatalf("complete open edition: step %+v, err %v", step, err)
	}
}

func TestPlanPublishRefusesOtherOpenScorecard(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaB}
	_, err := planPublish(open, nil, shaA, someCandidates(3))
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") || !strings.Contains(err.Error(), shortSHA(shaB)) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanPublishRefusesForeignOperatorInOpenEdition(t *testing.T) {
	open := &openEditionJSON{Number: 2, ScorecardSHA256: shaA, Operators: []string{testOperator(9)}}
	_, err := planPublish(open, nil, shaA, someCandidates(3))
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanPublishNothingWhenAlreadySealed(t *testing.T) {
	latest := &editionJSON{Number: 1, ScorecardSHA256: shaA}
	for _, c := range someCandidates(3) {
		latest.Candidates = append(latest.Candidates, candidateJSON{Operator: c.Operator})
	}
	step, err := planPublish(nil, latest, shaA, someCandidates(3))
	if err != nil || step.Kind != stepNothing || step.Edition != 1 {
		t.Fatalf("step %+v, err %v", step, err)
	}
}

func TestPlanPublishOpensWhenSealedEditionDiffers(t *testing.T) {
	latest := &editionJSON{Number: 1, ScorecardSHA256: shaA, Candidates: []candidateJSON{{Operator: testOperator(1)}}}
	step, err := planPublish(nil, latest, shaA, someCandidates(2)) // a new map entry matched one more
	if err != nil || step.Kind != stepOpen || len(step.Missing) != 2 {
		t.Fatalf("same scorecard, more candidates: step %+v, err %v", step, err)
	}
	step, err = planPublish(nil, latest, shaB, someCandidates(1)) // a corrected scorecard
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
		{publishStep{Kind: stepNothing, Edition: 1}, "already published as edition 1: nothing to send"},
	} {
		if got := c.step.describe(3); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
