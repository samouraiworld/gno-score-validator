package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/louisb/score-validator/internal/scoring"
)

// evidenceFile holds the GitHub evidence blocks, relative to data/.
const evidenceFile = "github/evidence.json"

// loadEvidence reads data/github/evidence.json, the GitHub evidence block of
// every linked validator, keyed by scoring.Key(moniker).
//
// The blocks are produced outside this repo by the GitHub sweep, and they are
// the only record of the all-time versus since-cutoff contribution split. They
// stay json.RawMessage so their fields reach scorecard.json untouched; the
// page's allowlist decides which of them get published.
func loadEvidence(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var byMoniker map[string]json.RawMessage
	if err := json.Unmarshal(b, &byMoniker); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[string]json.RawMessage{}
	for moniker, block := range byMoniker {
		k := scoring.Key(moniker)
		if k == "" {
			return nil, fmt.Errorf("%s: empty moniker", path)
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s: moniker %q appears twice", path, moniker)
		}
		if len(block) == 0 || string(block) == "null" {
			return nil, fmt.Errorf("%s: %s has no evidence block", path, moniker)
		}
		out[k] = block
	}
	return out, nil
}
