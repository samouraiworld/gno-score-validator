package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeGnokey writes a shell script standing in for gnokey: it records its
// arguments (NUL-separated) and the password line it reads, then succeeds or
// fails the way gnokey does when the realm panics.
func fakeGnokey(t *testing.T, fail bool) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\0' \"$@\" > '" + filepath.Join(dir, "args") + "'\n" +
		"IFS= read -r pw || true\n" +
		"printf '%s' \"$pw\" > '" + filepath.Join(dir, "password") + "'\n"
	if fail {
		script += "printf 'GAS USED: 1\\n--= Error =--\\nData: edition 2 is not open\\nMsg Traces:\\n'\nexit 1\n"
	} else {
		script += "echo OK!\n"
	}
	bin = filepath.Join(dir, "gnokey")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func TestGnokeyArgs(t *testing.T) {
	g := gnokeyCLI{Bin: "gnokey", Key: "score-publisher", Remote: "https://rpc.gno.land:443", ChainID: "gnoland-1",
		Realm: defaultRealm, GasFee: "200000ugnot", GasWanted: "80000000", MaxDeposit: "1000000ugnot"}
	want := []string{"maketx", "call", "-pkgpath", defaultRealm, "-func", "Seal", "-args", "1", "-args", "36",
		"-gas-fee", "200000ugnot", "-gas-wanted", "80000000", "-max-deposit", "1000000ugnot", "-broadcast",
		"-chainid", "gnoland-1", "-remote", "https://rpc.gno.land:443", "score-publisher"}
	if got := g.args("Seal", []string{"1", "36"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	pw := ""
	g.Home, g.Password = "/tmp/keys", &pw
	got := g.args("DiscardOpen", nil)
	if got[len(got)-1] != "score-publisher" || !strings.Contains(strings.Join(got, " "), "-home /tmp/keys -insecure-password-stdin score-publisher") {
		t.Fatalf("got %q", got)
	}
	g.MaxDeposit = ""
	if got := strings.Join(g.args("DiscardOpen", nil), " "); strings.Contains(got, "-max-deposit") {
		t.Fatalf("empty MaxDeposit still passed: %q", got)
	}
}

func TestGnokeyCallForwardsMaxDeposit(t *testing.T) {
	bin, dir := fakeGnokey(t, false)
	pw := ""
	g := gnokeyCLI{Bin: bin, Key: "k", Remote: "r", ChainID: "dev", Realm: defaultRealm,
		GasFee: "1ugnot", GasWanted: "1", MaxDeposit: "250000ugnot", Password: &pw, Out: io.Discard}
	if err := g.Call("Seal", "1", "3"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.ReplaceAll(string(raw), "\x00", " "); !strings.Contains(got, " -max-deposit 250000ugnot ") {
		t.Fatalf("gnokey did not receive -max-deposit 250000ugnot: %q", raw)
	}
}

func TestGnokeyCallPassesPayloadAndPassword(t *testing.T) {
	bin, dir := fakeGnokey(t, false)
	pw := "secret"
	g := gnokeyCLI{Bin: bin, Key: "k", Remote: "127.0.0.1:26657", ChainID: "dev", Realm: defaultRealm,
		GasFee: "1ugnot", GasWanted: "1", Password: &pw, Out: io.Discard}
	payload := testOperator(1) + "|A|5000|1|0|5000\n" + testOperator(2) + "|B|4000|2|0|4000"
	if err := g.Call("AddCandidates", "1", payload); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "args"))
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	found := false
	for _, a := range args {
		found = found || a == payload
	}
	if !found {
		t.Fatalf("payload not passed as one argument: %q", args)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "password")); string(got) != "secret" {
		t.Fatalf("password %q", got)
	}
}

func TestGnokeyCallReportsRealmError(t *testing.T) {
	bin, _ := fakeGnokey(t, true)
	pw := ""
	g := gnokeyCLI{Bin: bin, Key: "k", Remote: "r", ChainID: "dev", Realm: defaultRealm, GasFee: "1ugnot", GasWanted: "1", Password: &pw}
	err := g.Call("AddCandidates", "2", "x")
	if err == nil || !strings.Contains(err.Error(), "AddCandidates") || !strings.Contains(err.Error(), "edition 2 is not open") {
		t.Fatalf("err = %v", err)
	}
}
