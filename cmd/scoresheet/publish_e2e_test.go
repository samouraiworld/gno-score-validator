package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPublishOnGnodev runs publish against a real gnodev that serves the
// validator_scores realm and four seeded valopers. It is opt-in: set
// SCORESHEET_E2E_REALM_DIR to the realm directory (samcrew-deployer) and
// GNOROOT to the gno checkout gnodev, gnokey and gnoland were built from.
// gnodev loads a staged copy of the realm whose publisher.gno names the
// signing key (test1, or SCORESHEET_E2E_KEY): the realm hard-codes its
// publisher and never trusts the deployer.
// Run with -v to see each transaction's gas and storage fee.
func TestPublishOnGnodev(t *testing.T) {
	realmDir := os.Getenv("SCORESHEET_E2E_REALM_DIR")
	if realmDir == "" {
		t.Skip("SCORESHEET_E2E_REALM_DIR is not set")
	}
	if os.Getenv("GNOROOT") == "" {
		t.Skip("GNOROOT is not set")
	}
	for _, bin := range []string{"gnodev", "gnokey", "gnoland"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}
	realmDir, err := filepath.Abs(realmDir)
	if err != nil {
		t.Fatal(err)
	}
	key := os.Getenv("SCORESHEET_E2E_KEY")
	if key == "" {
		key = "test1"
	}
	publisher, ok := e2ePublishers[key]
	if !ok {
		t.Fatalf("SCORESHEET_E2E_KEY=%q: the seed keybase holds only test1 and seed1-seed4", key)
	}
	work := t.TempDir()
	remote := startGnodev(t, stageRealm(t, realmDir, work, publisher), work)

	home := filepath.Join(work, "keys")
	seed := exec.Command("bash", filepath.Join("..", "..", "scripts", "gnodev-seed.sh"))
	seed.Env = append(os.Environ(), "REMOTE="+remote, "SEED_HOME="+home)
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	chain := newRPCClient(remote)
	empty := ""
	tx := gnokeyCLI{Bin: "gnokey", Home: home, Key: key, Remote: remote, ChainID: "dev", Realm: defaultRealm,
		GasFee: "1000000ugnot", GasWanted: "80000000", MaxDeposit: "1000000ugnot", Password: &empty, Out: testLog{t}}
	var out bytes.Buffer
	opts := publishOptions{ChainID: "dev", Remote: remote, Realm: defaultRealm, Scorecard: fixtureScorecard, Batch: 2, Out: &out}

	// 1. -dry reads everything and sends nothing.
	opts.Dry = true
	if err := publishRun(opts, chain, tx); err != nil {
		t.Fatalf("dry: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "3 of 4 matched") || !strings.Contains(out.String(), "Unregistered Node") {
		t.Fatalf("dry report:\n%s", out.String())
	}
	if open, err := readOpenEdition(chain, defaultRealm); err != nil || open != nil {
		t.Fatalf("after -dry: open %+v, err %v", open, err)
	}

	// 2. An interrupted run: the edition is open and holds one candidate.
	raw, err := os.ReadFile(fixtureScorecard)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if err := tx.Call("OpenEdition", "2026-10-13T00:00:00Z", sha, "challenge,test13,topaz,sapphire,pearl,firedrill"); err != nil {
		t.Fatal(err)
	}
	open, err := readOpenEdition(chain, defaultRealm)
	if err != nil || open == nil {
		t.Fatalf("open edition %+v, err %v", open, err)
	}
	if err := tx.Call("AddCandidates", strconv.Itoa(open.Number), firstSeedLine(t, chain)); err != nil {
		t.Fatal(err)
	}

	// 3. The real run resumes, seals and reads the edition back.
	opts.Dry = false
	out.Reset()
	if err := publishRun(opts, chain, tx); err != nil {
		t.Fatalf("publish: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "resume open edition") || !strings.Contains(out.String(), "LatestJSON matches") {
		t.Fatalf("report:\n%s", out.String())
	}
	latest, err := readLatest(chain, defaultRealm)
	if err != nil || latest == nil {
		t.Fatalf("latest %+v, err %v", latest, err)
	}
	if latest.Number != open.Number || len(latest.Candidates) != 3 || latest.Candidates[0].Moniker != "seed alpha" ||
		latest.Candidates[0].Total != 9550 || latest.Candidates[1].Github != 370 {
		t.Fatalf("latest %+v", latest)
	}

	// 4. Running again sends nothing.
	out.Reset()
	if err := publishRun(opts, chain, tx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already published as edition") {
		t.Fatalf("rerun:\n%s", out.String())
	}
}

// e2ePublishers are the addresses of the keys the seed script puts in its
// keybase, derived from gnodev's public test1 mnemonic at HD indices 0-4
// (Step 2 of the plan checked seed1-seed4). The staged realm names one of
// them as its publisher.
var e2ePublishers = map[string]string{
	"test1": "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5",
	"seed1": "g1kcdd3n0d472g2p5l8svyg9t0wq6h5857nq992f",
	"seed2": "g1d598tyfatprdstalqutk62cnzpm3thvyy9mypg",
	"seed3": "g1w4qqmxdk59xsh3x5hnp2z78s4ymyva8pnenfem",
	"seed4": "g14supzhx0v5sza947sdh4x74wnws9xvcfwdecef",
}

// stageRealm copies the realm's gnomod.toml and .gno files, without the
// _test.gno files, into work/validator_scores and replaces publisher.gno with
// one that names publisher. The content is what the realm plan's staging
// command writes:
// printf 'package validator_scores\n\nconst initialPublisher address = "g1..."\n'
func stageRealm(t *testing.T, src, work, publisher string) string {
	t.Helper()
	dst := filepath.Join(work, "validator_scores")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasSuffix(name, "_test.gno") ||
			(name != "gnomod.toml" && !strings.HasSuffix(name, ".gno")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pub := "package validator_scores\n\nconst initialPublisher address = \"" + publisher + "\"\n"
	if err := os.WriteFile(filepath.Join(dst, "publisher.gno"), []byte(pub), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// startGnodev starts gnodev on a free port with the valopers realm and the
// realm under test, and returns its RPC address once it answers.
func startGnodev(t *testing.T, realmDir, work string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote := l.Addr().String()
	l.Close()
	logPath := filepath.Join(work, "gnodev.log")
	logf, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("gnodev", "local", "-no-web", "-no-watch", "-node-rpc-listener", remote,
		"-paths", valopersRealm, "-C", work, realmDir)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		logf.Close()
	})
	chain := newRPCClient(remote)
	deadline := time.Now().Add(90 * time.Second)
	for {
		if n, err := chain.Network(); err == nil && n == "dev" {
			return remote
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(logPath)
			lines := strings.Split(string(b), "\n")
			if len(lines) > 40 {
				lines = lines[len(lines)-40:]
			}
			t.Fatalf("gnodev did not answer on %s within 90s; log tail:\n%s", remote, strings.Join(lines, "\n"))
		}
		time.Sleep(time.Second)
	}
}

// firstSeedLine encodes the fixture's first candidate with its seeded
// operator address, the way publish would.
func firstSeedLine(t *testing.T, chain *rpcClient) string {
	t.Helper()
	card, err := loadSnapshot(fixtureScorecard)
	if err != nil {
		t.Fatal(err)
	}
	roster, err := fetchValopers(chain)
	if err != nil {
		t.Fatal(err)
	}
	arenas, err := editionArenas(card)
	if err != nil {
		t.Fatal(err)
	}
	v := card.Validators[0]
	for _, r := range roster {
		if normMoniker(r.Moniker) == normMoniker(v.Moniker) {
			c, err := buildCandidate(v, arenas, r.Operator)
			if err != nil {
				t.Fatal(err)
			}
			line, err := encodeLine(c)
			if err != nil {
				t.Fatal(err)
			}
			return line
		}
	}
	t.Fatalf("%s is not in the seeded roster", v.Moniker)
	return ""
}

// testLog sends gnokey's output to the test log, so -v shows gas and fees.
type testLog struct{ t *testing.T }

func (w testLog) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
