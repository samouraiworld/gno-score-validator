package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/louisb/score-validator/internal/scoring"
	"github.com/louisb/score-validator/internal/sheetx"
)

// The snapshot mirrors the spreadsheet into data/scorecard.json, which the
// standalone HTML page embeds. Field order matches the file it rewrites, so a
// refresh shows up as a diff of values and never of shape.
type snapshot struct {
	GeneratedAt   string          `json:"generated_at"`
	Source        string          `json:"source"`
	Weights       json.RawMessage `json:"weights"`
	GithubWeights json.RawMessage `json:"github_score_weights"`
	Validators    []snapValidator `json:"validators"`
}

type snapValidator struct {
	Moniker         string         `json:"moniker"`
	Discord         string         `json:"discord"`
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
	// cutoff. The sheet holds none of it and the CSVs only carry the all-time
	// half, so it is carried across untouched rather than rebuilt.
	Github json.RawMessage `json:"github,omitempty"`

	RankCurrent   int `json:"rank_current"`
	RankCorrected int `json:"rank_corrected"`
}

type snapComponents struct {
	Challenge float64 `json:"challenge"`
	Test13    float64 `json:"test13"`
	Topaz     float64 `json:"topaz"`
	Sapphire  float64 `json:"sapphire"`
	Pearl     float64 `json:"pearl"`
	FireDrill float64 `json:"firedrill"`
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

// payloadRe matches the one-line JSON island the HTML page reads at startup.
var payloadRe = regexp.MustCompile(
	`(?s)(<script id="payload" type="application/json">)(.*?)(</script>)`)

// runSnapshot rebuilds data/scorecard.json from the spreadsheet and re-embeds
// it in the HTML page, so the two can never disagree about a score.
//
// It merges rather than regenerates: everything the sheet holds is rewritten
// from the sheet, and everything it does not — the GitHub evidence blocks — is
// carried over from the existing file.
func runSnapshot(e *env, args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	dry := fs.Bool("dry", false, "print the plan without writing")
	out := fs.String("out", "", "snapshot file (default data/scorecard.json)")
	page := fs.String("html", "web/validator-scorecard.html", "page to re-embed it in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		*out = e.data("scorecard.json")
	}

	prev, err := loadSnapshot(*out)
	if err != nil {
		return err
	}
	carried := map[string]json.RawMessage{}
	before := map[string]float64{}
	for _, v := range prev.Validators {
		if len(v.Github) > 0 {
			carried[v.Moniker] = v.Github
		}
		before[v.Moniker] = v.ScoreCorrected
	}

	rows, err := e.sheetValues(fmt.Sprintf("%s!A%d:L%d",
		sheetx.Ref(scoring.TabBilan), scoring.FirstRow, scoring.LastRow))
	if err != nil {
		return err
	}
	challenge, err := e.challengeDetail()
	if err != nil {
		return err
	}
	firedrill, err := e.firedrillDetail()
	if err != nil {
		return err
	}
	onchain, err := e.onchainByNetwork()
	if err != nil {
		return err
	}

	next := snapshot{
		GeneratedAt:   time.Now().Format("2006-01-02"),
		Source:        prev.Source,
		Weights:       prev.Weights,
		GithubWeights: prev.GithubWeights,
	}

	for _, row := range rows {
		moniker := cell(row, 0)
		if moniker == "" {
			continue
		}
		v := snapValidator{
			Moniker: moniker,
			Discord: cell(row, 1),
			Components: snapComponents{
				Challenge: number(row, 2), Test13: number(row, 3),
				Topaz: number(row, 4), Sapphire: number(row, 5), Pearl: number(row, 6),
				FireDrill: number(row, 7),
			},
			GithubCorrected: number(row, 8),
			ScoreCorrected:  number(row, 9),
			Onchain:         map[string]snapOnchain{},
			Github:          carried[moniker],
		}
		if d, ok := challenge[moniker]; ok {
			v.Challenge = &d
		}
		if d, ok := firedrill[strings.ToLower(moniker)]; ok {
			v.FireDrillDetail = &d
		}
		for net, byMoniker := range onchain {
			if o, ok := byMoniker[strings.ToLower(moniker)]; ok {
				v.Onchain[net] = o
			}
		}

		// The uncorrected pair is what the score would be with no cutoff year
		// applied to GitHub. It is the page's "vs uncorrected" delta, and only
		// one candidate has ever carried one.
		v.GithubCurrent = v.GithubCorrected
		if len(v.Github) > 0 {
			var g struct {
				AllTime map[string]float64 `json:"all_time"`
			}
			if err := json.Unmarshal(v.Github, &g); err != nil {
				return fmt.Errorf("%s: github block: %w", moniker, err)
			}
			v.GithubCurrent = githubScore(g.AllTime)
		}
		v.ScoreCurrent = total(v.Components, v.GithubCurrent)

		next.Validators = append(next.Validators, v)
	}

	rank(next.Validators)

	changed := 0
	for _, v := range next.Validators {
		if old, ok := before[v.Moniker]; ok && absDiff(old, v.ScoreCorrected) > 0.005 {
			changed++
			fmt.Printf("  %-26s %6.2f -> %-6.2f  rank %d\n",
				v.Moniker, old, v.ScoreCorrected, v.RankCorrected)
		}
	}
	fmt.Printf("\n%d validators, %d scores changed, generated_at %s -> %s\n",
		len(next.Validators), changed, prev.GeneratedAt, next.GeneratedAt)
	if *dry {
		fmt.Println("-dry — nothing written.")
		return nil
	}

	pretty, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(pretty, '\n'), 0o644); err != nil {
		return err
	}

	compact, err := json.Marshal(next)
	if err != nil {
		return err
	}
	html, err := os.ReadFile(*page)
	if err != nil {
		return err
	}
	if !payloadRe.Match(html) {
		return fmt.Errorf(`%s: no <script id="payload"> block`, *page)
	}
	// Literal $ in the JSON would be read as a capture reference by Expand.
	embedded := payloadRe.ReplaceAllFunc(html, func(m []byte) []byte {
		g := payloadRe.FindSubmatch(m)
		return append(append(append([]byte{}, g[1]...), compact...), g[3]...)
	})
	if err := os.WriteFile(*page, embedded, 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s and re-embedded it in %s\n", *out, *page)
	return nil
}

func loadSnapshot(path string) (snapshot, error) {
	var s snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// githubScore applies the participation weights to a contribution breakdown.
func githubScore(c map[string]float64) float64 {
	return scoring.WeightMergedPR*c["merged_prs"] +
		scoring.WeightUnmergedPR*c["unmerged_prs"] +
		scoring.WeightIssue*c["issues"] +
		scoring.WeightCommit*c["commits"] +
		scoring.WeightComment*c["comment_threads"]
}

// total is the scoring rule, in Go rather than in fr_FR formula syntax.
func total(c snapComponents, github float64) float64 {
	t := (c.Challenge+c.Test13+c.Topaz+c.Sapphire+c.Pearl+c.FireDrill)/scoring.ArenaDivisor +
		github/scoring.BonusDivisor
	if t > scoring.MaxScore {
		return scoring.MaxScore
	}
	return t
}

// rank fills both rank fields, highest score first. Ties keep spreadsheet row
// order, which is the order the candidates were entered.
func rank(vs []snapValidator) {
	for _, spec := range []struct {
		by  func(snapValidator) float64
		set func(*snapValidator, int)
	}{
		{func(v snapValidator) float64 { return v.ScoreCurrent },
			func(v *snapValidator, r int) { v.RankCurrent = r }},
		{func(v snapValidator) float64 { return v.ScoreCorrected },
			func(v *snapValidator, r int) { v.RankCorrected = r }},
	} {
		idx := make([]int, len(vs))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			return spec.by(vs[idx[a]]) > spec.by(vs[idx[b]])
		})
		for r, i := range idx {
			spec.set(&vs[i], r+1)
		}
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

// challengeDetail reads the onboarding breakdown, keyed by moniker.
func (e *env) challengeDetail() (map[string]snapChallenge, error) {
	rows, err := e.sheetValues(fmt.Sprintf("%s!B2:H1000", sheetx.Ref(scoring.TabChallenge)))
	if err != nil {
		return nil, err
	}
	out := map[string]snapChallenge{}
	for _, r := range rows {
		moniker := cell(r, 0)
		if moniker == "" {
			continue
		}
		out[moniker] = snapChallenge{
			Weighted: number(r, 1), Simple: number(r, 2), Penalty: number(r, 3),
			Bonus: number(r, 4), FinalW: number(r, 5), Score: number(r, 6),
		}
	}
	return out, nil
}

// firedrillDetail reads the exercise rubric, keyed case-insensitively by the
// scorecard moniker. Portal spelling and the rotated exercise address are kept
// for provenance, never for joining.
func (e *env) firedrillDetail() (map[string]snapFireDrill, error) {
	rows, err := e.sheetValues(fmt.Sprintf("%s!A2:I1000", sheetx.Ref(scoring.TabFireDrill)))
	if err != nil {
		return nil, err
	}
	out := map[string]snapFireDrill{}
	for _, r := range rows {
		moniker := strings.ToLower(strings.TrimSpace(cell(r, 0)))
		if moniker == "" {
			continue
		}
		out[moniker] = snapFireDrill{
			UploadTime: optNumber(r, 3), Metadata: optNumber(r, 4), LogQuality: optNumber(r, 5),
			Incident: number(r, 6), Findings: cell(r, 7),
			PortalMoniker: cell(r, 1), Address: cell(r, 8),
		}
	}
	return out, nil
}

// optNumber is number for a column that is legitimately blank: it returns nil
// rather than 0, so a cell nobody has filled in cannot pass for a score of 0.
func optNumber(row []interface{}, i int) *float64 {
	if i >= len(row) {
		return nil
	}
	f, ok := row[i].(float64)
	if !ok {
		return nil
	}
	return &f
}

// onchainByNetwork reads the network tabs into network -> moniker -> row.
// The moniker key is lowercased because that is how Bilan's VLOOKUP matches it,
// and a tab can spell a validator "offellnode" where Bilan says "offellNode".
func (e *env) onchainByNetwork() (map[string]map[string]snapOnchain, error) {
	out := map[string]map[string]snapOnchain{}
	for _, tab := range scoring.Networks {
		rows, err := e.sheetValues(fmt.Sprintf("%s!A2:M1000", sheetx.Ref(tab)))
		if err != nil {
			return nil, err
		}
		net := strings.TrimPrefix(tab, "Score-")
		byMoniker := map[string]snapOnchain{}
		for _, r := range rows {
			moniker := strings.ToLower(strings.TrimSpace(cell(r, 0)))
			if moniker == "" {
				continue
			}
			o := snapOnchain{
				Address: cell(r, 1), Score: number(r, 2), Tier: cell(r, 3),
				Sign: number(r, 4), VP: number(r, 5),
				Critical: number(r, 7), Warning: number(r, 8), Freq: number(r, 9),
				FreqRate: number(r, 10), Downtime: number(r, 11), Missed: number(r, 12),
			}
			// A blank proposer means the validator was never asked to propose.
			// The page renders null as an em dash; a zero would read as failure.
			if len(r) > 6 {
				if f, ok := r[6].(float64); ok {
					o.Proposer = &f
				}
			}
			byMoniker[moniker] = o
		}
		out[net] = byMoniker
	}
	return out, nil
}

// cell returns a column as trimmed text, or "" when the row is short.
func cell(row []interface{}, i int) string {
	if i >= len(row) || row[i] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(row[i]))
}

// number returns a column as a float, or 0 when it is blank or not numeric.
func number(row []interface{}, i int) float64 {
	if i >= len(row) {
		return 0
	}
	f, _ := row[i].(float64)
	return f
}
