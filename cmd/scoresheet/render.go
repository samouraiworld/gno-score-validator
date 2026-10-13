package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/louisb/score-validator/internal/scoring"
)

// The scoreboard is the Markdown face of data/scorecard.json: GitHub renders
// it where it will not render the HTML page. It is generated whole from the
// snapshot, so it needs no spreadsheet and no credentials, and it changes only
// when the data does — the date it shows is the snapshot's, never "now".
//
// It carries public fields only: scores, on-chain metrics and the exercise
// rubric. Discord handles and the GitHub identity blocks stay out.
const (
	scoreboardDir = "scoreboard"
	validatorsDir = "validators"
)

// renderArena is one column of the scoreboard. present tells an absent
// candidate, who scores 0 by rule, apart from one who was measured at 0.
type renderArena struct {
	id      string
	label   string
	source  string
	detail  string
	inTotal bool
	score   func(snapValidator) float64
	present func(snapValidator) bool
}

func onchainPresent(net string) func(snapValidator) bool {
	return func(v snapValidator) bool { _, ok := v.Onchain[net]; return ok }
}

func snapshotRenderArenas(s snapshot) ([]renderArena, error) {
	if len(s.Arenas) == 0 {
		return nil, fmt.Errorf("scorecard has no arenas; regenerate it with `scoresheet score`")
	}
	out := make([]renderArena, 0, len(s.Arenas))
	for _, a := range s.Arenas {
		a := a
		present := func(v snapValidator) bool { return v.Components.Has(a.ID) }
		switch a.Detail {
		case "challenge":
			present = func(v snapValidator) bool { return v.Challenge != nil }
		case "onchain":
			present = onchainPresent(a.ID)
		case "firedrill":
			present = func(v snapValidator) bool { return v.FireDrillDetail != nil }
		}
		out = append(out, renderArena{
			id: a.ID, label: a.Label, source: a.Source, detail: a.Detail, inTotal: a.InTotal,
			score: func(v snapValidator) float64 { return v.Components.Get(a.ID) }, present: present,
		})
	}
	return out, nil
}

// Score bands, highest first.
var renderBands = []struct {
	min   float64
	emoji string
	label string
}{
	{80, "🟢", "80 and above"},
	{60, "🟡", "60 to 80"},
	{40, "🟠", "40 to 60"},
	{0, "🔴", "below 40"},
}

func band(score float64) int {
	for i, b := range renderBands {
		if score >= b.min {
			return i
		}
	}
	return len(renderBands) - 1
}

// runRender regenerates the scoreboard from data/scorecard.json.
func runRender(root string, args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	in := fs.String("in", "", "snapshot file (default data/scorecard.json)")
	page := fs.String("html", "", "page to re-embed the public payload in (default web/validator-scorecard.html)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		*in = filepath.Join(root, "data", "scorecard.json")
	}
	if *page == "" {
		*page = filepath.Join(root, "web", "validator-scorecard.html")
	}
	s, err := loadSnapshot(*in)
	if err != nil {
		return err
	}
	if _, err := snapshotRenderArenas(s); err != nil {
		return err
	}
	if err := embedPage(s, *page); err != nil {
		return err
	}
	fmt.Printf("re-embedded the public payload in %s\n", *page)
	return render(s, root)
}

// render writes scoreboard/README.md and one page per validator. The
// validators directory is generated whole: it is emptied first, so a
// candidate who leaves the scorecard does not leave a stale page behind.
func render(s snapshot, root string) error {
	arenas, err := snapshotRenderArenas(s)
	if err != nil {
		return err
	}
	// A component missing for a listed arena would render as absent, beside a
	// stored total that still counted it.
	for _, v := range s.Validators {
		for _, a := range s.Arenas {
			if !v.Components.Has(a.ID) {
				return fmt.Errorf("%s: no %s component; regenerate the scorecard with `scoresheet score`", v.Moniker, a.ID)
			}
		}
	}
	vs := append([]snapValidator(nil), s.Validators...)
	// Ranks follow from the scores, so they are recomputed rather than trusted:
	// a scorecard.json written before ties shared a rank still renders right.
	rank(vs)
	sort.SliceStable(vs, func(a, b int) bool { return vs[a].RankCorrected < vs[b].RankCorrected })

	slugs := map[string]string{}
	taken := map[string]string{}
	for _, v := range vs {
		sl := slug(v.Moniker)
		if other, dup := taken[sl]; dup {
			return fmt.Errorf("monikers %q and %q share the page name %s.md", other, v.Moniker, sl)
		}
		taken[sl] = v.Moniker
		slugs[v.Moniker] = sl
	}

	dir := filepath.Join(root, scoreboardDir)
	pages := filepath.Join(dir, validatorsDir)
	if err := os.RemoveAll(pages); err != nil {
		return err
	}
	if err := os.MkdirAll(pages, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), renderIndex(s, arenas, vs, slugs), 0o644); err != nil {
		return err
	}
	for _, v := range vs {
		page := renderValidator(s, arenas, v, len(vs))
		if err := os.WriteFile(filepath.Join(pages, slugs[v.Moniker]+".md"), page, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("rendered %s/README.md and %d validator pages\n", scoreboardDir, len(vs))
	return nil
}

// renderMainnet lists the mainnet report under the arenas. It sits apart from
// them on purpose: no total reads it.
func renderMainnet(b *bytes.Buffer, m *snapMainnet) {
	candidates := 0
	for _, r := range m.Validators {
		if r.Candidate {
			candidates++
		}
	}
	fmt.Fprintf(b, "\n## Mainnet `%s` — shown, not scored\n\n", m.Chain)
	fmt.Fprintf(b, "gnomonitoring `%s` report fetched **%s**: %d validators, %d of them candidates."+
		" No score above includes it; its weight will be decided when a candidate joins mainnet.\n\n",
		m.Period, m.FetchedAt, len(m.Validators), candidates)
	b.WriteString("| Validator | Candidate | Tier | Score | Signing % | Missed blocks |\n" +
		"| --- | --- | --- | ---: | ---: | ---: |\n")
	for _, r := range m.Validators {
		cand := ""
		if r.Candidate {
			cand = "yes"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s |\n",
			mdEscape(r.Moniker), cand, r.Tier, num(r.Score), num(r.Sign), num(r.Missed))
	}
}

const generatedNote = "<!-- Generated by `scoresheet render` from data/scorecard.json. Do not edit by hand. -->\n\n"

func renderIndex(s snapshot, arenas []renderArena, vs []snapValidator, slugs map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString(generatedNote)
	b.WriteString("# Gno validator scoreboard\n\n")

	scores := make([]float64, len(vs))
	for i, v := range vs {
		scores[i] = v.ScoreCorrected
	}
	fmt.Fprintf(&b, "Data as of **%s** · **%d** candidates · median score **%s**\n\n",
		s.GeneratedAt, len(vs), num(median(scores)))
	var counted []string
	for _, a := range arenas {
		if a.inTotal {
			counted = append(counted, a.id)
		}
	}
	fmt.Fprintf(&b, "**Score** = (%s) / %d"+
		" + GitHub / %d, capped at %d.\n\n", strings.Join(counted, " + "), len(counted), scoring.BonusDivisor, scoring.MaxScore)
	fmt.Fprintf(&b, "All %d arenas carry equal weight and are final: the testnets have ended and the", len(counted))
	b.WriteString(
		" fire drill has closed. A candidate absent from an arena scores 0 there. The GitHub" +
			" bonus counts contributions from 2025 on and is worth at most 10 points." +
			" Mainnet (`gnoland-1`) is shown below but not scored.\n\n")

	b.WriteString("## Arenas\n\n")
	b.WriteString("| Arena | Source | Candidates scored | Median |\n| --- | --- | ---: | ---: |\n")
	for _, a := range arenas {
		var got []float64
		for _, v := range vs {
			if a.present(v) {
				got = append(got, a.score(v))
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", a.label, a.source, len(got), num(median(got)))
	}

	if m := s.Mainnet; m != nil {
		renderMainnet(&b, m)
	}

	b.WriteString("\n## Distribution\n\n")
	counts := make([]int, len(renderBands))
	for _, v := range vs {
		counts[band(v.ScoreCorrected)]++
	}
	b.WriteString("| Band | Score | Candidates |\n| --- | --- | ---: |\n")
	for i, bd := range renderBands {
		fmt.Fprintf(&b, "| %s | %s | %d |\n", bd.emoji, bd.label, counts[i])
	}
	b.WriteString("\n```mermaid\npie showData title Candidates by score band\n")
	for i, bd := range renderBands {
		fmt.Fprintf(&b, "    %q : %d\n", bd.label, counts[i])
	}
	b.WriteString("```\n")

	b.WriteString("\n## Ranking\n\n")
	b.WriteString("`—` marks a candidate absent from that arena, which counts as 0." +
		" Equal scores share a rank.\n\n")
	b.WriteString("| # | Validator | Score | | ")
	for _, a := range arenas {
		b.WriteString(a.label + " | ")
	}
	b.WriteString("GitHub |\n| ---: | --- | ---: | --- | ")
	for range arenas {
		b.WriteString("---: | ")
	}
	b.WriteString("---: |\n")
	for _, v := range vs {
		fmt.Fprintf(&b, "| %d | %s [%s](%s/%s.md) | **%.2f** | `%s` | ",
			v.RankCorrected, renderBands[band(v.ScoreCorrected)].emoji,
			mdEscape(v.Moniker), validatorsDir, slugs[v.Moniker], v.ScoreCorrected, bar(v.ScoreCorrected))
		for _, a := range arenas {
			if a.present(v) {
				b.WriteString(num(a.score(v)))
			} else {
				b.WriteString("—")
			}
			b.WriteString(" | ")
		}
		fmt.Fprintf(&b, "%s |\n", num(v.GithubCorrected))
	}
	return b.Bytes()
}

func renderValidator(s snapshot, arenas []renderArena, v snapValidator, n int) []byte {
	var b bytes.Buffer
	b.WriteString(generatedNote)
	fmt.Fprintf(&b, "# %s\n\n", mdEscape(v.Moniker))
	fmt.Fprintf(&b, "[← Scoreboard](../README.md) · Data as of %s\n\n", s.GeneratedAt)
	fmt.Fprintf(&b, "%s **Rank %d of %d** · **Score %.2f / %d**\n\n",
		renderBands[band(v.ScoreCorrected)].emoji, v.RankCorrected, n, v.ScoreCorrected, scoring.MaxScore)

	b.WriteString("## Score breakdown\n\n")
	b.WriteString("| Arena | Arena score | Points |\n| --- | ---: | ---: |\n")
	divisor := 0
	for _, a := range arenas {
		if a.inTotal {
			divisor++
		}
	}
	for _, a := range arenas {
		score := "—"
		if a.present(v) {
			score = num(a.score(v))
		}
		points := 0.0
		if a.inTotal {
			points = a.score(v) / float64(divisor)
		}
		fmt.Fprintf(&b, "| %s | %s | %.2f |\n", a.label, score, points)
	}
	fmt.Fprintf(&b, "| GitHub bonus | %s | %.2f |\n", num(v.GithubCorrected), v.GithubCorrected/scoring.BonusDivisor)
	fmt.Fprintf(&b, "| **Total** | | **%.2f** |\n", v.ScoreCorrected)
	if total(v.Components, s.Arenas, v.GithubCorrected) >= scoring.MaxScore {
		fmt.Fprintf(&b, "\nThe points add up past %d, so the score is capped.\n", scoring.MaxScore)
	}
	b.WriteString("\n`—` means absent from that arena, which counts as 0.\n")

	var nets []string
	for _, a := range arenas {
		if a.detail == "onchain" {
			if _, ok := v.Onchain[a.id]; ok {
				nets = append(nets, a.id)
			}
		}
	}
	if len(nets) > 0 {
		b.WriteString("\n## On-chain metrics\n\n")
		b.WriteString("| Network | Score | Tier | Sign rate | Proposer | Missed | Downtime | Critical | Warning | Address |\n")
		b.WriteString("| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
		for _, net := range nets {
			o := v.Onchain[net]
			proposer := "—"
			if o.Proposer != nil {
				proposer = num(*o.Proposer)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | `%s` |\n",
				net, num(o.Score), o.Tier, num(o.Sign), proposer, num(o.Missed), num(o.Downtime),
				num(o.Critical), num(o.Warning), o.Address)
		}
		b.WriteString("\nA proposer of `—` means the validator was never asked to propose.\n")
	}

	if fd := v.FireDrillDetail; fd != nil {
		fireDrillScore := 0.0
		for _, a := range arenas {
			if a.detail == "firedrill" {
				fireDrillScore = a.score(v)
				break
			}
		}
		b.WriteString("\n## Fire drill\n\n")
		b.WriteString("Four criteria worth 25 each.\n\n")
		b.WriteString("| Upload time | Metadata | Log quality | Incident response | Total |\n| ---: | ---: | ---: | ---: | ---: |\n")
		fmt.Fprintf(&b, "| %s | %s | %s | %s | **%s** |\n",
			optNum(fd.UploadTime), optNum(fd.Metadata), optNum(fd.LogQuality), num(fd.Incident),
			num(fireDrillScore))
		if fd.Findings != "" {
			fmt.Fprintf(&b, "\n> %s\n", mdEscape(fd.Findings))
		}
	}

	if c := v.Challenge; c != nil {
		b.WriteString("\n## Onboarding challenge\n\n")
		b.WriteString("| Weighted | Simple | Penalty | Bonus | Final weight | Score |\n| ---: | ---: | ---: | ---: | ---: | ---: |\n")
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | **%s** |\n",
			num(c.Weighted), num(c.Simple), num(c.Penalty), num(c.Bonus), num(c.FinalW), num(c.Score))
	}
	return b.Bytes()
}

// num prints a value with at most two decimals and no trailing zeros.
func num(f float64) string {
	return strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
}

func optNum(f *float64) string {
	if f == nil {
		return "—"
	}
	return num(*f)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// bar draws a score out of 100 as ten blocks.
func bar(score float64) string {
	full := int(math.Round(score / 10))
	full = max(0, min(10, full))
	return strings.Repeat("█", full) + strings.Repeat("░", 10-full)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug turns a moniker into a page name: lowercase, dashes for anything else.
func slug(moniker string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(moniker), "-"), "-")
	if s == "" {
		return "validator"
	}
	return s
}

var mdSpecial = strings.NewReplacer(
	`\`, `\\`, "|", `\|`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
	"`", "\\`", "<", "&lt;", ">", "&gt;", "\n", " ")

// mdEscape keeps a moniker or a note from being read as Markdown syntax or
// from breaking a table row.
func mdEscape(s string) string { return mdSpecial.Replace(s) }
