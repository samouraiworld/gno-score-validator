// Package monitoring reads validator reliability metrics from gnomonitoring.
//
// The service exposes one report per chain, each validator carrying the same
// eleven metrics over four periods. The CSV snapshots under
// gno-monitoring-metrics/ are this payload flattened, one column per
// period-metric pair; only sapphire-1 is still live, so the other files are
// frozen backups that no request can refresh.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// BaseURL is the gnomonitoring reports endpoint.
const BaseURL = "https://gnomonitoring.samourai.live/api/reports/validators"

// Periods a report carries. The scoresheet scores the whole testnet run, so it
// reads current_year; the shorter windows are there for triage.
const (
	PeriodLast24h = "last_24h"
	PeriodWeek    = "current_week"
	PeriodMonth   = "current_month"
	PeriodYear    = "current_year"
)

// Metrics is one validator's numbers over one period.
//
// Every count is an integer in the payload, but proposer reliability is null
// for a validator that has not been asked to propose, which is why it is a
// pointer: a missing turn is not a zero score.
type Metrics struct {
	Score        float64  `json:"score"`
	Tier         string   `json:"tier"`
	SignRate     float64  `json:"sign_rate"`
	Proposer     *float64 `json:"proposer_reliability"`
	VotingPower  float64  `json:"voting_power"`
	Critical     float64  `json:"critical_count"`
	Warning      float64  `json:"warning_count"`
	Incidents    float64  `json:"incident_count"`
	IncidentRate float64  `json:"incident_rate_per_week"`
	Downtime     float64  `json:"downtime_blocks"`
	Missed       float64  `json:"missed_blocks"`
}

// Validator is one line of a report.
type Validator struct {
	Address string             `json:"addr"`
	Moniker string             `json:"moniker"`
	Periods map[string]Metrics `json:"periods"`
}

// Fetch returns the report for a chain, e.g. "sapphire-1". An unknown chain is
// a 400 with the reason in the body, so the body is quoted in the error.
func Fetch(ctx context.Context, chain string) ([]Validator, error) {
	u := BaseURL + "?" + url.Values{"chain": {chain}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body [512]byte
		n, _ := resp.Body.Read(body[:])
		return nil, fmt.Errorf("%s: %s: %q", chain, resp.Status, body[:n])
	}

	var out []Validator
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", chain, err)
	}
	return out, nil
}
