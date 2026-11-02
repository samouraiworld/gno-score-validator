package main

import (
	"fmt"
	"slices"
)

type stepKind int

const (
	stepOpen    stepKind = iota // open a new edition and add every candidate
	stepResume                  // finish the open edition of this scorecard
	stepNothing                 // the latest sealed edition already holds this publication
)

type publishStep struct {
	Kind       stepKind
	Edition    int            // stepResume: the open edition; stepNothing: the sealed one
	Missing    []pubCandidate // candidates still to add
	Supersedes int            // stepOpen: the sealed edition of this scorecard whose lines differ; 0 for none
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
// publication, every line compared, is already the latest sealed edition
// sends nothing; a sealed edition of this scorecard with other lines is
// superseded by a new one.
func planPublish(open *openEditionJSON, latest *editionJSON, meta editionMeta, cands []pubCandidate) (publishStep, error) {
	sha := meta.SHA256
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
	if latest != nil && latest.ScorecardSHA256 == sha {
		lines := make([]string, len(cands))
		for i, c := range cands {
			line, err := encodeLine(c)
			if err != nil {
				return publishStep{}, err
			}
			lines[i] = line
		}
		if sealedDiff(latest, meta, lines) == nil {
			return publishStep{Kind: stepNothing, Edition: latest.Number}, nil
		}
		return publishStep{Kind: stepOpen, Missing: cands, Supersedes: latest.Number}, nil
	}
	return publishStep{Kind: stepOpen, Missing: cands}, nil
}

// sealedDiff compares a sealed edition, field by field, with an edition's
// metadata and lines; nil means they are the same publication.
func sealedDiff(e *editionJSON, meta editionMeta, lines []string) error {
	if e.ScorecardSHA256 != meta.SHA256 || e.ComputedAt != meta.ComputedAt || !slices.Equal(e.Arenas, meta.Arenas) {
		return fmt.Errorf("edition %d on chain: metadata differs", e.Number)
	}
	want := slices.Clone(lines)
	got := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		line, err := encodeLine(pubCandidate{Operator: c.Operator, Moniker: c.Moniker, Total: c.Total, Rank: c.Rank, Github: c.Github, Arenas: c.ArenaScores})
		if err != nil {
			return fmt.Errorf("edition %d on chain: %w", e.Number, err)
		}
		got = append(got, line)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("edition %d on chain holds %d candidates that differ from the %d of this run", e.Number, len(got), len(want))
	}
	return nil
}

// describe is the plan line of the report; count is the run's candidate count.
func (s publishStep) describe(count int) string {
	switch s.Kind {
	case stepNothing:
		return fmt.Sprintf("already published as edition %d (LatestJSON matches this run): nothing to send", s.Edition)
	case stepResume:
		return fmt.Sprintf("resume open edition %d: add %d missing candidate(s), seal with %d", s.Edition, len(s.Missing), count)
	default:
		if s.Supersedes > 0 {
			return fmt.Sprintf("open a new edition, add %d candidate(s), seal with %d (sealed edition %d holds this scorecard with other lines)", len(s.Missing), count, s.Supersedes)
		}
		return fmt.Sprintf("open a new edition, add %d candidate(s), seal with %d", len(s.Missing), count)
	}
}
