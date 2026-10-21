package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// chainProfile is what publish needs to know about a chain.
type chainProfile struct {
	ChainID    string
	Remote     string
	MapPath    string // relative to the repo root; "" for no override file
	GasFee     string
	GasWanted  string
	MaxDeposit string // gnokey -max-deposit: the storage deposit ceiling of one call
}

// chainProfiles are the chains publish knows. The gnoland-1 gas values follow
// samcrew-deployer's config/networks.toml (2026-09-23): the fee is never
// refunded, so it is sized from the live gas price rather than guessed high.
// Storage costs about 100 ugnot per byte on both chains; the realm plan
// measured about 3.2 KB per AddCandidates of 20, so 1 GNOT caps a batch of 10
// (about 0.16 GNOT) with room to spare. valoper-map.csv holds gnoland-1
// addresses, so dev reads no map.
var chainProfiles = map[string]chainProfile{
	"dev": {ChainID: "dev", Remote: "http://127.0.0.1:26657", GasFee: "1000000ugnot", GasWanted: "80000000",
		MaxDeposit: "1000000ugnot"},
	"gnoland-1": {ChainID: "gnoland-1", Remote: "https://rpc.gno.land:443", MapPath: filepath.Join("data", "valoper-map.csv"),
		GasFee: "200000ugnot", GasWanted: "80000000", MaxDeposit: "1000000ugnot"},
}

// defaultBatch is the number of candidates per AddCandidates call. The realm
// plan measured 60-65M gas for 20 candidates on gnodev at the gnoland-1
// runtime; 10 stays near half of the 80M gas wanted.
const defaultBatch = 10

type publishConfig struct {
	opts          publishOptions
	tx            gnokeyCLI
	passwordStdin bool
	checkFresh    bool // the scorecard is data/scorecard.json and must match the CSVs
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func parsePublishFlags(root string, args []string) (publishConfig, error) {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	chain := fs.String("chain", "", "dev or gnoland-1 (required)")
	remote := fs.String("remote", "", "node RPC URL (default: the chain's)")
	key := fs.String("key", "score-publisher", "gnokey key that signs")
	realm := fs.String("realm", defaultRealm, "realm package path")
	dry := fs.Bool("dry", false, "print the plan, send nothing")
	scorecard := fs.String("scorecard", "", "scorecard to publish (default: data/scorecard.json, checked against the CSVs)")
	mapPath := fs.String("map", "", "moniker,operator_address overrides (default: data/valoper-map.csv on gnoland-1, none on dev)")
	batch := fs.Int("batch", defaultBatch, "candidates per AddCandidates call")
	gasFee := fs.String("gas-fee", "", "gas fee per transaction (default: the chain's)")
	gasWanted := fs.String("gas-wanted", "", "gas wanted per transaction (default: the chain's)")
	maxDeposit := fs.String("max-deposit", "", "storage deposit ceiling per transaction, gnokey -max-deposit (default: the chain's)")
	home := fs.String("gnokey-home", "", "gnokey home directory (default: gnokey's)")
	pwStdin := fs.Bool("insecure-password-stdin", false, "read the key password once from stdin; for test keys")
	if err := fs.Parse(args); err != nil {
		return publishConfig{}, err
	}
	if fs.NArg() > 0 {
		return publishConfig{}, fmt.Errorf("publish takes no argument, got %q", fs.Arg(0))
	}
	p, ok := chainProfiles[*chain]
	if !ok {
		return publishConfig{}, fmt.Errorf("publish needs -chain dev or -chain gnoland-1, got %q", *chain)
	}
	if *batch < 1 {
		return publishConfig{}, fmt.Errorf("-batch must be at least 1")
	}
	r := orDefault(*remote, p.Remote)
	card, checkFresh := filepath.Join(root, "data", "scorecard.json"), true
	if *scorecard != "" {
		card, checkFresh = *scorecard, false
	}
	m := ""
	if p.MapPath != "" {
		m = filepath.Join(root, p.MapPath)
	}
	m = orDefault(*mapPath, m)
	return publishConfig{
		opts: publishOptions{ChainID: p.ChainID, Remote: r, Realm: *realm, Scorecard: card, MapPath: m,
			Batch: *batch, Dry: *dry, Out: os.Stdout},
		tx: gnokeyCLI{Bin: "gnokey", Home: *home, Key: *key, Remote: r, ChainID: p.ChainID, Realm: *realm,
			GasFee: orDefault(*gasFee, p.GasFee), GasWanted: orDefault(*gasWanted, p.GasWanted),
			MaxDeposit: orDefault(*maxDeposit, p.MaxDeposit), Out: os.Stdout},
		passwordStdin: *pwStdin,
		checkFresh:    checkFresh,
	}, nil
}

// runPublish is `scoresheet publish`.
func runPublish(root string, args []string) error {
	cfg, err := parsePublishFlags(root, args)
	if err != nil {
		return err
	}
	if cfg.checkFresh {
		card, err := loadSnapshot(cfg.opts.Scorecard)
		if err != nil {
			return err
		}
		if err := checkScorecardFresh(root, card); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cfg.opts.Out, "note       %s is not data/scorecard.json: freshness check skipped\n", cfg.opts.Scorecard)
	}
	if !cfg.opts.Dry {
		if _, err := exec.LookPath(cfg.tx.Bin); err != nil {
			return fmt.Errorf("gnokey is not installed: %w", err)
		}
		if cfg.passwordStdin {
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			pw := strings.TrimRight(line, "\r\n")
			cfg.tx.Password = &pw
		}
	}
	return publishRun(cfg.opts, newRPCClient(cfg.opts.Remote), cfg.tx)
}
