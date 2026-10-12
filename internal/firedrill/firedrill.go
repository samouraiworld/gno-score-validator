// Package firedrill reads the validator fire drill portal's two exports and
// turns them into one scored row per validator.
//
// The portal keeps the two halves apart: scores.json holds the four rubric
// criteria keyed by a random submission id that carries no moniker, and
// submissions.jsonl holds {id, moniker, operator_address, filename,
// submitted_at}. Neither is usable alone — the id is the only join, and the
// rubric split is not recoverable from the published total.
package firedrill

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// IncidentFloor is the score given where the portal awards the bottom tier of
// the incident-response rubric.
//
// The portal holds 5. The exercise announced itself (2 Sep 16:00 UTC) *after*
// its own investigation window had closed (1 Sep 12:00 UTC), in a channel
// validators do not monitor, so a missing log was partly the exercise's fault
// and the floor was raised to 8. The two sources disagree by 3 points on the
// affected rows by design; do not "correct" this back to the portal's number.
const (
	IncidentFloor       = 8
	IncidentPortalFloor = 5
)

// Score is one submission's rubric, each criterion worth 25.
type Score struct {
	SubmissionID    string `json:"submission_id"`
	Scored          bool   `json:"scored"`
	GenesisMatch    bool   `json:"genesis_match"`
	VersionSupport  bool   `json:"version_supported"`
	LogWindow       Window `json:"log_window"`
	SentryLogWindow Window `json:"sentry_log_window"`
	UploadTime      int    `json:"upload_time_score"`
	Metadata        int    `json:"metadata_score"`
	LogQuality      int    `json:"log_quality_score"`
	Incident        int    `json:"incident_response_quality_score"`
}

// Window says whether timestamps were found in a log, and whether they span
// the investigation window.
type Window struct {
	Detected bool `json:"detected"`
	Covered  bool `json:"covered"`
}

// Submission is one uploaded archive.
type Submission struct {
	ID        string `json:"id"`
	Moniker   string `json:"moniker"`
	Address   string `json:"operator_address"`
	Filename  string `json:"filename"`
	Submitted string `json:"submitted_at"`
}

// Result pairs a submission with its rubric and the total it earns.
type Result struct {
	Submission
	Score
	Total int
}

// IncidentAdjusted is the incident-response score as this repo scores it,
// with the portal's bottom tier raised. This, not the portal's raw number, is
// what the sheet stores — the stored criteria must sum to the stored total.
func (s Score) IncidentAdjusted() int {
	if s.Incident == IncidentPortalFloor {
		return IncidentFloor
	}
	return s.Incident
}

// Total sums the four criteria as stored.
func (s Score) Total() int {
	return s.UploadTime + s.Metadata + s.LogQuality + s.IncidentAdjusted()
}

// AutoFindings describes what the automated checks flagged, in the vocabulary
// the existing rows use. It deliberately says nothing about conduct: the
// incident-response half of a finding is a human judgement written by hand and
// must never be generated.
//
// A log with no usable timestamps cannot be told apart here from one that is
// simply empty — the portal reports both as an undetected window — so this
// returns the weaker claim of the two.
func (s Score) AutoFindings() string {
	var out []string
	if !s.GenesisMatch {
		out = append(out, "genesis_sha256 does not match the expected value")
	}
	if !s.VersionSupport {
		out = append(out, "gnoland_version is not in the supported list")
	}
	switch {
	case !s.LogWindow.Detected:
		out = append(out, "no recognizable timestamps")
	case !s.LogWindow.Covered:
		out = append(out, "logs do not fully cover the window")
	}
	return strings.Join(out, " · ")
}

// Load joins the two exports and returns the best submission per moniker.
//
// A validator may upload more than once. The best attempt counts, which is
// also the earliest one in every case seen so far: both re-submissions landed
// after the deadline and scored worse, and a late second try does not erase a
// good one made on time.
func Load(scoresPath, submissionsPath string) ([]Result, error) {
	blob, err := os.ReadFile(scoresPath)
	if err != nil {
		return nil, err
	}
	var scores map[string]Score
	if err := json.Unmarshal(blob, &scores); err != nil {
		return nil, fmt.Errorf("%s: %w", scoresPath, err)
	}

	f, err := os.Open(submissionsPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	best := map[string]Result{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var sub Submission
		if err := json.Unmarshal([]byte(text), &sub); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", submissionsPath, line, err)
		}
		score, ok := scores[sub.ID]
		if !ok {
			return nil, fmt.Errorf("%s:%d: %s has no entry in %s",
				submissionsPath, line, sub.Moniker, scoresPath)
		}
		r := Result{Submission: sub, Score: score, Total: score.Total()}
		if cur, seen := best[sub.Moniker]; !seen || r.Total > cur.Total {
			best[sub.Moniker] = r
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Moniker) < strings.ToLower(out[j].Moniker)
	})
	return out, nil
}
