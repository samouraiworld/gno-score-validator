package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/louisb/score-validator/internal/arena"
	"github.com/louisb/score-validator/internal/scoring"
)

// The snapshot mirrors the spreadsheet into data/scorecard.json, which the
// standalone HTML page embeds. Field order matches the file it rewrites, so a
// refresh shows up as a diff of values and never of shape.
type snapshot struct {
	GeneratedAt   string          `json:"generated_at"`
	Source        string          `json:"source"`
	Arenas        []snapArena     `json:"arenas"`
	Weights       json.RawMessage `json:"weights"`
	GithubWeights json.RawMessage `json:"github_score_weights"`
	Validators    []snapValidator `json:"validators"`
	// Shown beside the scorecard, never inside a total; see mainnet.go.
	Mainnet *snapMainnet `json:"mainnet,omitempty"`
}

type snapValidator struct {
	Moniker         string         `json:"moniker"`
	Discord         string         `json:"discord,omitempty"`
	Components      snapComponents `json:"components"`
	GithubCurrent   float64        `json:"github_current"`
	GithubCorrected float64        `json:"github_corrected"`
	ScoreCurrent    float64        `json:"score_current"`
	ScoreCorrected  float64        `json:"score_corrected"`
	Challenge       *snapChallenge `json:"challenge_detail"`
	// The total belongs in Components; this field carries only the rubric detail
	// so there cannot be a second, nullable representation of the same score.
	FireDrillDetail *snapFireDrill         `json:"fire_drill_detail,omitempty"`
	Onchain         map[string]snapOnchain `json:"onchain"`

	// Evidence gathered outside the spreadsheet: the account behind a
	// validator, and its contribution counts split all-time versus since the
	// cutoff. The sheet holds none of it; it is copied untouched from
	// data/github/evidence.json.
	Github json.RawMessage `json:"github,omitempty"`

	RankCurrent   int `json:"rank_current"`
	RankCorrected int `json:"rank_corrected"`
}

type snapArena struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Source  string `json:"source"`
	Colour  string `json:"colour"`
	InTotal bool   `json:"in_total"`
	Detail  string `json:"detail"`
}

type snapComponent struct {
	ID    string
	Score float64
}

// snapComponents is an ordered object: JSON keeps the compact public shape,
// while Go keeps config order instead of routing known ids into fixed fields.
type snapComponents []snapComponent

func (c snapComponents) Get(id string) float64 {
	for _, component := range c {
		if component.ID == id {
			return component.Score
		}
	}
	return 0
}

func (c snapComponents) Has(id string) bool {
	for _, component := range c {
		if component.ID == id {
			return true
		}
	}
	return false
}

func (c snapComponents) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, component := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		id, err := json.Marshal(component.ID)
		if err != nil {
			return nil, err
		}
		score, err := json.Marshal(component.Score)
		if err != nil {
			return nil, err
		}
		b.Write(id)
		b.WriteByte(':')
		b.Write(score)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (c *snapComponents) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("components must be an object")
	}
	var out snapComponents
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		id := tok.(string)
		if seen[id] {
			return fmt.Errorf("component %q appears twice", id)
		}
		seen[id] = true
		var score float64
		if err := dec.Decode(&score); err != nil {
			return fmt.Errorf("component %q: %w", id, err)
		}
		out = append(out, snapComponent{ID: id, Score: score})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected data after components")
		}
		return err
	}
	*c = out
	return nil
}

type snapChallenge struct {
	Weighted float64 `json:"weighted"`
	Simple   float64 `json:"simple"`
	Penalty  float64 `json:"penalty"`
	Bonus    float64 `json:"bonus"`
	FinalW   float64 `json:"final_w"`
	Score    float64 `json:"score"`
}

// snapFireDrill is the exercise rubric: four criteria worth 25 each, summing to
// the arena score. The three automatic ones stay pointers even though every row
// carries them today — the sheet is hand-maintained, and a blank cell must read
// as "not recorded" rather than as a zero, the same distinction Proposer draws.
type snapFireDrill struct {
	UploadTime    *float64 `json:"upload_time"`
	Metadata      *float64 `json:"metadata"`
	LogQuality    *float64 `json:"log_quality"`
	Incident      float64  `json:"incident"`
	Findings      string   `json:"findings,omitempty"`
	PortalMoniker string   `json:"portal_moniker"`
	Address       string   `json:"address"`
}

type snapOnchain struct {
	Address  string   `json:"address"`
	Tier     string   `json:"tier"`
	Score    float64  `json:"score"`
	Sign     float64  `json:"sign"`
	VP       float64  `json:"vp"`
	Proposer *float64 `json:"proposer"`
	Critical float64  `json:"critical"`
	Warning  float64  `json:"warning"`
	Freq     float64  `json:"freq"`
	FreqRate float64  `json:"freq_rate"`
	Downtime float64  `json:"downtime"`
	Missed   float64  `json:"missed"`
}

// publish prints how the scores moved since prev, then writes next to out,
// re-embeds it in the page and regenerates the scoreboard — unless dry.
func publish(prev, next snapshot, out, page string, dry bool, root string) error {
	before := map[string]float64{}
	for _, v := range prev.Validators {
		before[scoring.Key(v.Moniker)] = v.ScoreCorrected
	}
	changed := 0
	for _, v := range next.Validators {
		if old, ok := before[scoring.Key(v.Moniker)]; ok && absDiff(old, v.ScoreCorrected) > 0.005 {
			changed++
			fmt.Printf("  %-26s %6.2f -> %-6.2f  rank %d\n",
				v.Moniker, old, v.ScoreCorrected, v.RankCorrected)
		}
	}
	fmt.Printf("\n%d validators, %d scores changed, generated_at %s -> %s\n",
		len(next.Validators), changed, prev.GeneratedAt, next.GeneratedAt)
	if dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}

	if err := saveSnapshot(out, next); err != nil {
		return err
	}
	if err := embedPage(next, page); err != nil {
		return err
	}
	fmt.Printf("wrote %s and re-embedded it in %s\n", out, page)
	return render(next, root)
}

// attach copies a validator's challenge, fire drill and onchain detail onto
// it, if the arenas hold any.
func (d details) attach(v *snapValidator) {
	k := scoring.Key(v.Moniker)
	if c, ok := d.challenge[k]; ok {
		v.Challenge = &c
	}
	if f, ok := d.firedrill[k]; ok {
		v.FireDrillDetail = &f
	}
	for net, byMoniker := range d.onchain {
		if o, ok := byMoniker[k]; ok {
			v.Onchain[net] = o
		}
	}
}

func loadSnapshot(path string) (snapshot, error) {
	var s snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// total is the scoring rule over the snapshot's components that count.
func total(c snapComponents, arenas []snapArena, github float64) float64 {
	var scores []float64
	for _, a := range arenas {
		if a.InTotal {
			scores = append(scores, c.Get(a.ID))
		}
	}
	return scoring.Total(scores, github)
}

func snapshotArenas(cfg arena.Config) []snapArena {
	out := make([]snapArena, len(cfg.Arenas))
	for i, a := range cfg.Arenas {
		out[i] = snapArena{ID: a.ID, Label: a.Label, Source: a.Source, Colour: a.Colour, InTotal: a.InTotal, Detail: a.Detail}
	}
	return out
}

func snapshotWeights(cfg arena.Config) (json.RawMessage, error) {
	weights := make(snapComponents, 0, cfg.Divisor()+1)
	for _, a := range cfg.Arenas {
		if a.InTotal {
			weights = append(weights, snapComponent{ID: a.ID, Score: 1 / float64(cfg.Divisor())})
		}
	}
	weights = append(weights, snapComponent{ID: "github_bonus", Score: 1 / float64(scoring.BonusDivisor)})
	return json.Marshal(weights)
}

// rank fills both rank fields with scoring.Rank.
func rank(vs []snapValidator) {
	current := make([]float64, len(vs))
	corrected := make([]float64, len(vs))
	for i, v := range vs {
		current[i], corrected[i] = v.ScoreCurrent, v.ScoreCorrected
	}
	rc, rk := scoring.Rank(current), scoring.Rank(corrected)
	for i := range vs {
		vs[i].RankCurrent, vs[i].RankCorrected = rc[i], rk[i]
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// sheetValues reads a range unformatted, so numbers arrive as numbers rather
// than as fr_FR strings with a comma decimal separator.
func (e *env) sheetValues(rng string) ([][]interface{}, error) {
	vr, err := e.svc.Spreadsheets.Values.Get(e.id, rng).
		ValueRenderOption("UNFORMATTED_VALUE").Do()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rng, err)
	}
	return vr.Values, nil
}
