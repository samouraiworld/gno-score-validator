package main

import (
	"path/filepath"
	"testing"
)

func TestParsePublishFlagsMainnetDefaults(t *testing.T) {
	cfg, err := parsePublishFlags("/repo", []string{"-chain", "gnoland-1"})
	if err != nil {
		t.Fatal(err)
	}
	o, tx := cfg.opts, cfg.tx
	if o.ChainID != "gnoland-1" || o.Remote != "https://rpc.gno.land:443" || o.Realm != defaultRealm || o.Batch != 10 || o.Dry {
		t.Fatalf("opts %+v", o)
	}
	if o.Scorecard != filepath.Join("/repo", "data", "scorecard.json") || !cfg.checkFresh {
		t.Fatalf("scorecard %q, checkFresh %v", o.Scorecard, cfg.checkFresh)
	}
	if o.MapPath != filepath.Join("/repo", "data", "valoper-map.csv") {
		t.Fatalf("map %q", o.MapPath)
	}
	if tx.Key != "score-publisher" || tx.GasFee != "200000ugnot" || tx.GasWanted != "80000000" || tx.MaxDeposit != "1000000ugnot" ||
		tx.Remote != o.Remote || tx.ChainID != "gnoland-1" || tx.Password != nil {
		t.Fatalf("tx %+v", tx)
	}
}

func TestParsePublishFlagsDevOverrides(t *testing.T) {
	cfg, err := parsePublishFlags("/repo", []string{"-chain", "dev", "-remote", "127.0.0.1:36657", "-scorecard", "x.json",
		"-batch", "5", "-key", "test1", "-gnokey-home", "/tmp/k", "-insecure-password-stdin", "-max-deposit", "500000ugnot", "-dry"})
	if err != nil {
		t.Fatal(err)
	}
	o, tx := cfg.opts, cfg.tx
	if o.ChainID != "dev" || o.Remote != "127.0.0.1:36657" || o.MapPath != "" || o.Scorecard != "x.json" || o.Batch != 5 || !o.Dry {
		t.Fatalf("opts %+v", o)
	}
	if cfg.checkFresh || !cfg.passwordStdin || tx.Key != "test1" || tx.Home != "/tmp/k" || tx.Remote != "127.0.0.1:36657" || tx.GasFee != "1000000ugnot" {
		t.Fatalf("cfg %+v", cfg)
	}
	if tx.MaxDeposit != "500000ugnot" {
		t.Fatalf("-max-deposit not forwarded to gnokey: %q", tx.MaxDeposit)
	}
}

func TestParsePublishFlagsRefuses(t *testing.T) {
	for name, args := range map[string][]string{
		"no chain":      {},
		"unknown chain": {"-chain", "test13"},
		"betanet id":    {"-chain", "gnoland1"},
		"zero batch":    {"-chain", "dev", "-batch", "0"},
		"stray arg":     {"-chain", "dev", "extra"},
	} {
		if _, err := parsePublishFlags("/repo", args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
