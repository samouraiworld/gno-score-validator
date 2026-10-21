package main

import "fmt"

type stepKind int

const (
	stepOpen    stepKind = iota // open a new edition and add every candidate
	stepResume                  // finish the open edition of this scorecard
	stepNothing                 // the latest sealed edition already holds this publication
)

type publishStep struct {
	Kind    stepKind
	Edition int            // stepResume: the open edition; stepNothing: the sealed one
	Missing []pubCandidate // candidates still to add
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// planPublish decides what a run sends from what the realm holds. An open
// edition is finished only when it belongs to this scorecard and holds
// nothing this run would not publish; anything else needs DiscardOpen() by
// hand, because a sealed edition can never be corrected. A run whose exact
// publication is already the latest sealed edition sends nothing.
func planPublish(open *openEditionJSON, latest *editionJSON, sha string, cands []pubCandidate) (publishStep, error) {
	want := map[string]bool{}
	for _, c := range cands {
		want[c.Operator] = true
	}
	if open != nil {
		if open.ScorecardSHA256 != sha {
			return publishStep{}, fmt.Errorf("edition %d is open for scorecard %s, not this one (%s): call DiscardOpen() with the publisher key, then publish again",
				open.Number, shortSHA(open.ScorecardSHA256), shortSHA(sha))
		}
		have := map[string]bool{}
		for _, op := range open.Operators {
			if !want[op] {
				return publishStep{}, fmt.Errorf("open edition %d holds %s, which this run would not publish: call DiscardOpen() with the publisher key, then publish again",
					open.Number, op)
			}
			have[op] = true
		}
		var missing []pubCandidate
		for _, c := range cands {
			if !have[c.Operator] {
				missing = append(missing, c)
			}
		}
		return publishStep{Kind: stepResume, Edition: open.Number, Missing: missing}, nil
	}
	if latest != nil && latest.ScorecardSHA256 == sha && len(latest.Candidates) == len(cands) {
		same := true
		for _, c := range latest.Candidates {
			if !want[c.Operator] {
				same = false
				break
			}
		}
		if same {
			return publishStep{Kind: stepNothing, Edition: latest.Number}, nil
		}
	}
	return publishStep{Kind: stepOpen, Missing: cands}, nil
}

// describe is the plan line of the report; count is the run's candidate count.
func (s publishStep) describe(count int) string {
	switch s.Kind {
	case stepNothing:
		return fmt.Sprintf("already published as edition %d: nothing to send", s.Edition)
	case stepResume:
		return fmt.Sprintf("resume open edition %d: add %d missing candidate(s), seal with %d", s.Edition, len(s.Missing), count)
	default:
		return fmt.Sprintf("open a new edition, add %d candidate(s), seal with %d", len(s.Missing), count)
	}
}
