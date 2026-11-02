package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// publishOptions is one publish run, resolved from the flags.
type publishOptions struct {
	ChainID   string    // chain id the node must report
	Remote    string    // node RPC URL, for the report
	Realm     string    // validator_scores package path
	Scorecard string    // scorecard file to publish
	MapPath   string    // moniker,operator_address overrides; "" for none
	Batch     int       // candidates per AddCandidates call
	Dry       bool      // print the plan, send nothing
	Out       io.Writer // the report
}

// maxEditionCandidates caps one edition. The realm sets no cap, but its Render
// and EditionJSON exceed the node's query gas limit near 350 candidates with
// 16 arenas, and a sealed edition can never be fixed. 200 leaves about a 2x
// margin even at 16 arenas.
const maxEditionCandidates = 200

// chainReader is everything publish reads from the chain.
type chainReader interface {
	renderer
	realmReader
	Network() (string, error)
}

// publishRun publishes the scorecard's candidates that have a valoper record
// as one sealed edition: it checks the chain, matches monikers to operators,
// builds and guards the payload once, prints the plan, then (unless dry)
// opens or resumes the edition, adds the missing candidates in batches, seals
// it and reads it back. A failed transaction is never retried in the same
// run: it may have landed, so the operator runs publish again, which re-reads
// the chain and resumes.
func publishRun(o publishOptions, chain chainReader, tx txSender) error {
	raw, err := os.ReadFile(o.Scorecard)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	var card snapshot
	if err := json.Unmarshal(raw, &card); err != nil {
		return fmt.Errorf("%s: %w", o.Scorecard, err)
	}

	network, err := chain.Network()
	if err != nil {
		return err
	}
	if network != o.ChainID {
		return fmt.Errorf("%s serves chain %q, not %q: nothing read or sent", o.Remote, network, o.ChainID)
	}

	meta, err := newEditionMeta(card, sha)
	if err != nil {
		return err
	}
	if err := guardMeta(meta); err != nil {
		return err
	}

	roster, err := fetchValopers(chain)
	if err != nil {
		return err
	}
	overrides := map[string]string{}
	if o.MapPath != "" {
		if overrides, err = loadValoperMap(o.MapPath); err != nil {
			return err
		}
	}
	monikers := make([]string, len(card.Validators))
	for i, v := range card.Validators {
		monikers[i] = v.Moniker
	}
	matched, unmatched, err := matchOperators(monikers, roster, overrides)
	if err != nil {
		return err
	}

	var cands []pubCandidate
	var rows []matchRow
	viaMap := 0
	for _, v := range card.Validators {
		k := normMoniker(v.Moniker)
		op, ok := matched[k]
		if !ok {
			continue
		}
		_, mapped := overrides[k]
		if mapped {
			viaMap++
		}
		// Check the moniker first: the realm refuses it, and no later error
		// may echo it.
		if err := checkMoniker(collapseSpaces(v.Moniker)); err != nil {
			return fmt.Errorf("candidate ranked %d: %w; nothing sent", v.RankCorrected, err)
		}
		c, err := buildCandidate(v, meta.Arenas, op)
		if err != nil {
			return err
		}
		cands = append(cands, c)
		rows = append(rows, matchRow{Rank: c.Rank, Moniker: c.Moniker, Operator: op, ViaMap: mapped})
	}
	slices.SortStableFunc(rows, func(a, b matchRow) int { return a.Rank - b.Rank })
	// The one encoding of the run: the report, the send and the read-back all
	// use these lines.
	lines, err := payloadLines(cands)
	if err != nil {
		return err
	}
	if len(lines) > maxEditionCandidates {
		return fmt.Errorf("%d candidates matched a valoper; an edition takes at most %d, or the realm could not render it once sealed: nothing sent",
			len(lines), maxEditionCandidates)
	}
	if _, err := batches(lines, o.Batch); err != nil {
		return err
	}

	// Read the chain before reporting, so the report sizes what will be sent.
	var step publishStep
	var chainErr error
	if len(cands) > 0 {
		step, chainErr = planFromChain(o, chain, sha, cands)
	}
	toSend := lines
	if chainErr == nil && len(cands) > 0 {
		toSend = linesFor(lines, step)
	}
	payloads, _ := batches(toSend, o.Batch)
	printPublishReport(o, meta, len(card.Validators), len(cands), viaMap, rows, unmatched, payloads, len(toSend))

	if len(cands) == 0 {
		if o.Dry {
			fmt.Fprintln(o.Out, "plan       no candidate matched a valoper: nothing to publish\n-dry — nothing sent.")
			return nil
		}
		return fmt.Errorf("no candidate matched a valoper: nothing to publish")
	}
	if chainErr != nil {
		if o.Dry && isRealmMissing(chainErr) {
			fmt.Fprintf(o.Out, "plan       %s is not deployed on %s: nothing to compare with\n-dry — nothing sent.\n", o.Realm, o.ChainID)
			return nil
		}
		return chainErr
	}
	fmt.Fprintf(o.Out, "plan       %s\n", step.describe(len(cands)))
	if step.Kind == stepResume {
		// OpenEditionJSON lists operators only: a line already added is never
		// compared with this run's, and a stale one would show only after Seal.
		fmt.Fprintln(o.Out, "note       the lines already in the open edition are not checked again: if the valoper map or\n"+
			"           r/gnops/valopers changed since the first run, call DiscardOpen() and publish again")
	}
	if o.Dry {
		fmt.Fprintln(o.Out, "-dry — nothing sent.")
		return nil
	}
	if step.Kind == stepNothing {
		return nil
	}
	return sendEdition(o, chain, tx, meta, step, payloads, lines)
}

// planFromChain reads the realm and decides what this run sends.
func planFromChain(o publishOptions, chain realmReader, sha string, cands []pubCandidate) (publishStep, error) {
	open, err := readOpenEdition(chain, o.Realm)
	if err != nil {
		return publishStep{}, err
	}
	latest, err := readLatest(chain, o.Realm)
	if err != nil {
		return publishStep{}, err
	}
	return planPublish(open, latest, sha, cands)
}

// linesFor picks, from the run's encoded lines and in their order, those the
// step still has to send.
func linesFor(lines []string, step publishStep) []string {
	if step.Kind == stepNothing {
		return nil
	}
	missing := map[string]bool{}
	for _, c := range step.Missing {
		missing[c.Operator] = true
	}
	var out []string
	for _, l := range lines {
		if op, _, _ := strings.Cut(l, "|"); missing[op] {
			out = append(out, l)
		}
	}
	return out
}

// matchRow is one matched candidate in the report.
type matchRow struct {
	Rank     int
	Moniker  string
	Operator string
	ViaMap   bool // the operator came from the valoper map, not from the moniker
}

func printPublishReport(o publishOptions, meta editionMeta, total, matched, viaMap int, rows []matchRow, unmatched []unmatchedCandidate, payloads []string, lineCount int) {
	w := o.Out
	fmt.Fprintf(w, "chain      %s (%s)\n", o.ChainID, o.Remote)
	fmt.Fprintf(w, "realm      %s\n", o.Realm)
	fmt.Fprintf(w, "edition    computedAt %s, scorecard sha256 %s\n", meta.ComputedAt, meta.SHA256)
	fmt.Fprintf(w, "arenas     %s\n", strings.Join(meta.Arenas, ","))
	fmt.Fprintf(w, "candidates %d of %d matched a valoper (%d through the valoper map)\n", matched, total, viaMap)
	for _, r := range rows {
		mark := ""
		if r.ViaMap {
			mark = "  (valoper map)"
		}
		fmt.Fprintf(w, "  %3d  %-28s -> %s%s\n", r.Rank, r.Moniker, r.Operator, mark)
	}
	fmt.Fprintf(w, "unmatched  %d, not published:\n", len(unmatched))
	for _, u := range unmatched {
		fmt.Fprintf(w, "  %-28s %s\n", u.Moniker, u.Reason)
	}
	size := 0
	for _, p := range payloads {
		size += len(p)
	}
	fmt.Fprintf(w, "payload    %d bytes in %d AddCandidates call(s) of up to %d candidates (%d line(s))\n", size, len(payloads), o.Batch, lineCount)
}

// sendEdition opens the edition when needed, adds the missing candidates
// batch by batch and seals it. Any failure stops the run without a retry,
// because a failed call may still have landed; running publish again resumes
// from what the chain holds. payloads are the batches of the missing lines;
// lines are all the run's lines, for the read-back.
func sendEdition(o publishOptions, chain realmReader, tx txSender, meta editionMeta, step publishStep, payloads, lines []string) error {
	edition := step.Edition
	if step.Kind == stepOpen {
		if err := tx.Call("OpenEdition", meta.ComputedAt, meta.SHA256, strings.Join(meta.Arenas, ",")); err != nil {
			return fmt.Errorf("OpenEdition: %w (it may still have landed: run publish again to resume)", err)
		}
		open, err := readOpenEdition(chain, o.Realm)
		if err != nil {
			return fmt.Errorf("reading the edition after OpenEdition: %w (run publish again to resume)", err)
		}
		if open == nil || open.ScorecardSHA256 != meta.SHA256 {
			return fmt.Errorf("OpenEdition went through but OpenEditionJSON does not show this scorecard's edition")
		}
		edition = open.Number
		fmt.Fprintf(o.Out, "opened edition %d\n", edition)
	}
	for i, p := range payloads {
		if err := tx.Call("AddCandidates", strconv.Itoa(edition), p); err != nil {
			return fmt.Errorf("batch %d of %d: %w (it may still have landed: run publish again to resume)", i+1, len(payloads), err)
		}
		fmt.Fprintf(o.Out, "added batch %d of %d\n", i+1, len(payloads))
	}
	if err := tx.Call("Seal", strconv.Itoa(edition), strconv.Itoa(len(lines))); err != nil {
		return fmt.Errorf("seal: %w (it may still have landed: run publish again to resume)", err)
	}
	return verifySealed(o, chain, edition, meta, lines)
}

// verifySealed reads the latest edition back and compares it, field by
// field, with the lines that were sent.
func verifySealed(o publishOptions, chain realmReader, edition int, meta editionMeta, lines []string) error {
	latest, err := readLatest(chain, o.Realm)
	if err != nil {
		return fmt.Errorf("reading the sealed edition back: %w (run publish again to resume)", err)
	}
	if latest == nil || latest.Number != edition {
		return fmt.Errorf("LatestJSON does not show edition %d after Seal", edition)
	}
	if latest.ScorecardSHA256 != meta.SHA256 || latest.ComputedAt != meta.ComputedAt || !slices.Equal(latest.Arenas, meta.Arenas) {
		return fmt.Errorf("edition %d on chain: metadata differs from what was sent", edition)
	}
	want := slices.Clone(lines)
	got := make([]string, 0, len(latest.Candidates))
	for _, c := range latest.Candidates {
		line, err := encodeLine(pubCandidate{Operator: c.Operator, Moniker: c.Moniker, Total: c.Total, Rank: c.Rank, Github: c.Github, Arenas: c.ArenaScores})
		if err != nil {
			return fmt.Errorf("edition %d on chain: %w", edition, err)
		}
		got = append(got, line)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("edition %d on chain holds %d candidates that differ from the %d sent", edition, len(got), len(want))
	}
	fmt.Fprintf(o.Out, "sealed edition %d with %d candidates; LatestJSON matches what was sent\n", edition, len(lines))
	return nil
}
