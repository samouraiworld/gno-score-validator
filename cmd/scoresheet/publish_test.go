package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// fakeChain plays both the node and gnokey: it renders a valoper roster and
// keeps the realm's editions in memory, applying OpenEdition, AddCandidates
// and Seal as the realm's spec says.
type fakeChain struct {
	network      string
	roster       string
	open         *fakeEdition
	sealed       []fakeEdition
	calls        []string
	sent         []string // every argument of every call
	failOn       string
	missingRealm bool
}

type fakeEdition struct {
	number     int
	computedAt string
	sha        string
	arenas     []string
	lines      []string
}

func newFakeChain() *fakeChain {
	return &fakeChain{network: "dev", roster: "# Welcome to the **Valopers** realm\n\n" +
		rosterLine("Seed Alpha", testOperator(1)) + rosterLine("seed-bravo", testOperator(2)) +
		rosterLine("Seed_Charlie", testOperator(3)) + rosterLine("Seed Delta", testOperator(4))}
}

func (f *fakeChain) Network() (string, error) { return f.network, nil }

func (f *fakeChain) Render(pkgPath, path string) (string, error) {
	if pkgPath != valopersRealm || path != "" {
		return "", fmt.Errorf("unexpected render %s:%s", pkgPath, path)
	}
	return f.roster, nil
}

func (f *fakeChain) Eval(_, expr string) (string, error) {
	if f.missingRealm {
		return "", &abciError{Type: "/vm.InvalidPkgPathError", Detail: "package not found"}
	}
	switch expr {
	case "OpenEditionJSON()":
		if f.open == nil {
			return "null", nil
		}
		ops := []string{}
		for _, l := range f.open.lines {
			ops = append(ops, strings.SplitN(l, "|", 2)[0])
		}
		b, _ := json.Marshal(openEditionJSON{Number: f.open.number, ScorecardSHA256: f.open.sha, Operators: ops})
		return string(b), nil
	case "LatestJSON()":
		if len(f.sealed) == 0 {
			return "null", nil
		}
		e := f.sealed[len(f.sealed)-1]
		out := editionJSON{Number: e.number, ComputedAt: e.computedAt, ScorecardSHA256: e.sha, Arenas: e.arenas, Candidates: []candidateJSON{}}
		for _, l := range e.lines {
			out.Candidates = append(out.Candidates, parseFakeLine(l))
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	}
	return "", fmt.Errorf("unexpected eval %s", expr)
}

func parseFakeLine(l string) candidateJSON {
	f := strings.Split(l, "|")
	atoi := func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
	c := candidateJSON{Operator: f[0], Moniker: f[1], Total: atoi(f[2]), Rank: int(atoi(f[3])), Github: atoi(f[4])}
	for _, s := range strings.Split(f[5], ",") {
		c.ArenaScores = append(c.ArenaScores, atoi(s))
	}
	return c
}

func (f *fakeChain) Call(fn string, args ...string) error {
	f.calls = append(f.calls, fn)
	f.sent = append(f.sent, args...)
	if fn == f.failOn {
		return fmt.Errorf("gnokey %s: exit status 1: simulated failure", fn)
	}
	switch fn {
	case "OpenEdition":
		if f.open != nil {
			return fmt.Errorf("an edition is already open")
		}
		f.open = &fakeEdition{number: len(f.sealed) + 1, computedAt: args[0], sha: args[1], arenas: strings.Split(args[2], ",")}
	case "AddCandidates":
		if f.open == nil || args[0] != strconv.Itoa(f.open.number) {
			return fmt.Errorf("edition %s is not open", args[0])
		}
		f.open.lines = append(f.open.lines, strings.Split(args[1], "\n")...)
	case "Seal":
		if f.open == nil || args[0] != strconv.Itoa(f.open.number) {
			return fmt.Errorf("edition %s is not open", args[0])
		}
		if strconv.Itoa(len(f.open.lines)) != args[1] {
			return fmt.Errorf("edition holds %d candidates, not %s", len(f.open.lines), args[1])
		}
		f.sealed = append(f.sealed, *f.open)
		f.open = nil
	default:
		return fmt.Errorf("unexpected call %s", fn)
	}
	return nil
}

var fixtureScorecard = filepath.Join("testdata", "publish", "scorecard.json")

func fixtureSHA(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(fixtureScorecard)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func fixtureOptions(out *bytes.Buffer) publishOptions {
	return publishOptions{ChainID: "dev", Remote: "fake", Realm: defaultRealm, Scorecard: fixtureScorecard, Batch: 2, Out: out}
}

var fixtureLines = []string{
	testOperator(1) + "|seed alpha|9550|1|0|10000,8500,9600,9600,10000,9600",
	testOperator(2) + "|Seed-Bravo|8870|2|370|9000,8000,8500,8800,9200,7500",
	testOperator(3) + "|Seed_Charlie|5583|3|0|7000,6000,6500,7200,6800,0",
}

func TestPublishDryPrintsPlanAndSendsNothing(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	o := fixtureOptions(out)
	o.Dry = true
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3 of 4 matched", "Unregistered Node", reasonNotRegistered,
		"open a new edition, add 3 candidate(s), seal with 3", "-dry", "2026-10-13T00:00:00Z", fixtureSHA(t)} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if len(chain.calls) != 0 {
		t.Fatalf("-dry sent %v", chain.calls)
	}
}

func TestPublishOpensAddsSealsAndVerifies(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	if err := publishRun(fixtureOptions(out), chain, chain); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := []string{"OpenEdition", "AddCandidates", "AddCandidates", "Seal"}; !reflect.DeepEqual(chain.calls, want) {
		t.Fatalf("calls %v, want %v", chain.calls, want)
	}
	if want := []string{"2026-10-13T00:00:00Z", fixtureSHA(t), "challenge,test13,topaz,sapphire,pearl,firedrill"}; !reflect.DeepEqual(chain.sent[:3], want) {
		t.Fatalf("OpenEdition args %q, want %q", chain.sent[:3], want)
	}
	if len(chain.sealed) != 1 || !reflect.DeepEqual(chain.sealed[0].lines, fixtureLines) {
		t.Fatalf("sealed %+v", chain.sealed)
	}
	if !strings.Contains(out.String(), "LatestJSON matches what was sent") {
		t.Fatalf("no verification line:\n%s", out)
	}
}

func TestPublishSendsNoPrivateField(t *testing.T) {
	chain := newFakeChain()
	if err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain); err != nil {
		t.Fatal(err)
	}
	for _, arg := range chain.sent {
		if strings.Contains(arg, "dc_") || strings.Contains(arg, "@") || strings.Contains(arg, "Unregistered") {
			t.Fatalf("sent %q", arg)
		}
	}
}

func TestPublishResumesOpenEdition(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	chain.open = &fakeEdition{number: 1, computedAt: "2026-10-13T00:00:00Z", sha: fixtureSHA(t),
		arenas: strings.Split("challenge,test13,topaz,sapphire,pearl,firedrill", ","), lines: fixtureLines[:1]}
	if err := publishRun(fixtureOptions(out), chain, chain); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := []string{"AddCandidates", "Seal"}; !reflect.DeepEqual(chain.calls, want) {
		t.Fatalf("calls %v, want %v", chain.calls, want)
	}
	if !strings.Contains(out.String(), "resume open edition 1") || !reflect.DeepEqual(chain.sealed[0].lines, fixtureLines) {
		t.Fatalf("sealed %+v\n%s", chain.sealed, out)
	}
	if !strings.Contains(out.String(), "not checked again") || !strings.Contains(out.String(), "call DiscardOpen() and publish again") {
		t.Fatalf("resume printed no DiscardOpen note:\n%s", out)
	}
}

func TestPublishPrintsResumeNoteOnlyWhenResuming(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	if err := publishRun(fixtureOptions(out), chain, chain); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "not checked again") {
		t.Fatalf("a new edition printed the resume note:\n%s", out)
	}
	chain, out = newFakeChain(), &bytes.Buffer{}
	chain.open = &fakeEdition{number: 1, sha: fixtureSHA(t), lines: fixtureLines[:1]}
	o := fixtureOptions(out)
	o.Dry = true
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not checked again") || len(chain.calls) != 0 {
		t.Fatalf("-dry resume: calls %v\n%s", chain.calls, out)
	}
}

// bigScorecard writes a scorecard of n made-up candidates named node-1..node-n
// and returns its path with a roster that registers all of them.
func bigScorecard(t *testing.T, n int) (string, string) {
	t.Helper()
	var vals []string
	var roster strings.Builder
	for i := 1; i <= n; i++ {
		vals = append(vals, fmt.Sprintf(`{"moniker": "node-%d", "components": {"challenge": 50}, "github_corrected": 0, "score_corrected": 50, "rank_corrected": %d}`, i, i))
		roster.WriteString(rosterLine(fmt.Sprintf("node-%d", i), testOperator(i)))
	}
	card := `{"generated_at": "2026-10-13", "arenas": [{"id": "challenge", "in_total": true}], "validators": [` + strings.Join(vals, ",") + `]}`
	path := filepath.Join(t.TempDir(), "scorecard.json")
	if err := os.WriteFile(path, []byte(card), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, roster.String()
}

func TestPublishCapsCandidatesPerEdition(t *testing.T) {
	path, roster := bigScorecard(t, maxEditionCandidates)
	chain, out := newFakeChain(), &bytes.Buffer{}
	chain.roster = roster
	o := fixtureOptions(out)
	o.Scorecard, o.Dry = path, true
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatalf("%d candidates refused: %v", maxEditionCandidates, err)
	}

	path, roster = bigScorecard(t, maxEditionCandidates+1)
	chain = newFakeChain()
	chain.roster = roster
	o = fixtureOptions(&bytes.Buffer{})
	o.Scorecard = path
	err := publishRun(o, chain, chain)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("at most %d", maxEditionCandidates)) || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
}

func TestPublishRefusesOpenEditionOfAnotherScorecard(t *testing.T) {
	chain := newFakeChain()
	chain.open = &fakeEdition{number: 1, sha: strings.Repeat("a", 64)}
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
}

func TestPublishRefusesOpenEditionWithForeignOperator(t *testing.T) {
	chain := newFakeChain()
	chain.open = &fakeEdition{number: 1, sha: fixtureSHA(t), lines: []string{testOperator(4) + "|Seed Delta|1|9|0|1,1,1,1,1,1"}}
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), "DiscardOpen") || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
}

func TestPublishSendsNothingWhenAlreadyPublished(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	if err := publishRun(fixtureOptions(out), chain, chain); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := publishRun(fixtureOptions(out), chain, chain); err != nil {
		t.Fatal(err)
	}
	if len(chain.calls) != 4 || len(chain.sealed) != 1 || !strings.Contains(out.String(), "already published as edition 1") {
		t.Fatalf("calls %v, sealed %d\n%s", chain.calls, len(chain.sealed), out)
	}
}

func TestPublishRefusesWrongNetwork(t *testing.T) {
	chain := newFakeChain()
	chain.network = "gnoland1"
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), `serves chain "gnoland1"`) || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
}

func TestPublishStopsOnFailedBatch(t *testing.T) {
	chain := newFakeChain()
	chain.failOn = "AddCandidates"
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), "run publish again to resume") {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"OpenEdition", "AddCandidates"}; !reflect.DeepEqual(chain.calls, want) {
		t.Fatalf("calls %v, want %v (no Seal after a failed batch)", chain.calls, want)
	}
	chain.failOn = ""
	chain.calls = nil
	if err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain); err != nil {
		t.Fatalf("resume after failure: %v", err)
	}
	if !reflect.DeepEqual(chain.sealed[0].lines, fixtureLines) {
		t.Fatalf("sealed %+v", chain.sealed)
	}
}

func TestPublishDryToleratesMissingRealm(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	chain.missingRealm = true
	o := fixtureOptions(out)
	o.Dry = true
	if err := publishRun(o, chain, chain); err != nil || !strings.Contains(out.String(), "is not deployed") {
		t.Fatalf("dry: err %v\n%s", err, out)
	}
	o.Dry = false
	if err := publishRun(o, chain, chain); !isRealmMissing(err) {
		t.Fatalf("real run: err = %v, want a missing-realm error", err)
	}
}

func TestPublishRefusesWhenNothingMatches(t *testing.T) {
	chain := newFakeChain()
	chain.roster = "# Welcome\n\nNo valopers to display."
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), "nothing to publish") || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
}

func TestPublishResumeReportsSizeOfWhatWillBeSent(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	chain.open = &fakeEdition{number: 1, computedAt: "2026-10-13T00:00:00Z", sha: fixtureSHA(t),
		arenas: strings.Split("challenge,test13,topaz,sapphire,pearl,firedrill", ","), lines: fixtureLines[:1]}
	o := fixtureOptions(out)
	o.Dry = true
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatal(err)
	}
	missing := strings.Join(fixtureLines[1:], "\n")
	want := fmt.Sprintf("payload    %d bytes in 1 AddCandidates call(s) of up to 2 candidates (2 line(s))", len(missing))
	if !strings.Contains(out.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, out)
	}
}

func TestPublishNeverRetriesAFailedOpen(t *testing.T) {
	chain := newFakeChain()
	chain.failOn = "OpenEdition"
	err := publishRun(fixtureOptions(&bytes.Buffer{}), chain, chain)
	if err == nil || !strings.Contains(err.Error(), "run publish again") {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"OpenEdition"}; !reflect.DeepEqual(chain.calls, want) {
		t.Fatalf("calls %v, want %v", chain.calls, want)
	}
}

func TestPublishRefusesRefusedMonikerBeforeAnySend(t *testing.T) {
	const bad = "Seed\u202eAlpha"
	card, err := os.ReadFile(fixtureScorecard)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scorecard.json")
	if err := os.WriteFile(path, []byte(strings.Replace(string(card), "seed  alpha", `Seed\u202eAlpha`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	chain, out := newFakeChain(), &bytes.Buffer{}
	chain.roster = rosterLine(bad, testOperator(1)) + rosterLine("seed-bravo", testOperator(2))
	o := fixtureOptions(out)
	o.Scorecard = path
	err = publishRun(o, chain, chain)
	if err == nil || !strings.Contains(err.Error(), "moniker") || len(chain.calls) != 0 {
		t.Fatalf("err %v, calls %v", err, chain.calls)
	}
	if strings.Contains(err.Error(), bad) || strings.Contains(out.String(), "\u202e") {
		t.Fatalf("the refused moniker was echoed: %v\n%q", err, out)
	}
}

func TestPublishRefusesOutOfBoundScoreBeforeAnySend(t *testing.T) {
	card, err := os.ReadFile(fixtureScorecard)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new, want string }{
		{"total above 100", `"score_corrected": 95.5`, `"score_corrected": 100.01`, "total 10001"},
		{"arena above 100", `"pearl": 100,`, `"pearl": 100.01,`, "arena score 10001"},
		{"bonus above 10", `"github_corrected": 0, "score_current": 95.5`, `"github_corrected": 100.1, "score_current": 95.5`, "GitHub bonus 1001"},
	} {
		if strings.Count(string(card), tc.old) != 1 {
			t.Fatalf("%s: %q is not unique in the fixture", tc.name, tc.old)
		}
		path := filepath.Join(t.TempDir(), "scorecard.json")
		if err := os.WriteFile(path, []byte(strings.Replace(string(card), tc.old, tc.new, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		chain, out := newFakeChain(), &bytes.Buffer{}
		o := fixtureOptions(out)
		o.Scorecard = path
		err := publishRun(o, chain, chain)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(chain.calls) != 0 {
			t.Errorf("%s: err %v, calls %v", tc.name, err, chain.calls)
		}
	}
}

func TestPublishReportListsEachMatchAndItsSource(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "map.csv")
	csv := "moniker,operator_address\nUnregistered Node," + testOperator(4) + "\n"
	if err := os.WriteFile(mapPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	chain, out := newFakeChain(), &bytes.Buffer{}
	o := fixtureOptions(out)
	o.Dry, o.MapPath = true, mapPath
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatal(err)
	}
	var alpha, mapped string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Contains(l, "->") && strings.Contains(l, testOperator(1)) {
			alpha = l
		}
		if strings.Contains(l, "->") && strings.Contains(l, testOperator(4)) {
			mapped = l
		}
	}
	if !strings.Contains(alpha, "seed alpha") || strings.Contains(alpha, "valoper map") {
		t.Errorf("moniker-matched line: %q", alpha)
	}
	if !strings.Contains(mapped, "Unregistered Node") || !strings.Contains(mapped, "(valoper map)") {
		t.Errorf("map-matched line: %q", mapped)
	}
	if strings.Index(out.String(), testOperator(1)) > strings.Index(out.String(), "payload ") {
		t.Errorf("match lines must precede the payload line:\n%s", out)
	}
}

func TestPublishReportMatchLinesAreInRankOrder(t *testing.T) {
	chain, out := newFakeChain(), &bytes.Buffer{}
	o := fixtureOptions(out)
	o.Dry = true
	if err := publishRun(o, chain, chain); err != nil {
		t.Fatal(err)
	}
	a, b, c := strings.Index(out.String(), testOperator(1)), strings.Index(out.String(), testOperator(2)), strings.Index(out.String(), testOperator(3))
	if !(a < b && b < c) {
		t.Errorf("match lines not in rank order:\n%s", out)
	}
}
