package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// txSender sends one call to the validator_scores realm.
type txSender interface {
	Call(fn string, args ...string) error
}

// gnokeyCLI sends calls through the gnokey binary, so the key stays in
// gnokey's keybase and this repo needs no gno library.
type gnokeyCLI struct {
	Bin       string // gnokey binary
	Home      string // -home; "" keeps gnokey's default keybase
	Key       string // key name or address that signs
	Remote    string
	ChainID   string
	Realm     string
	GasFee    string
	GasWanted string
	// MaxDeposit caps the storage deposit of one call (-max-deposit); "" leaves
	// the flag out.
	MaxDeposit string
	// Password, when set, goes to gnokey on stdin with -insecure-password-stdin.
	// Unset, gnokey asks for it on the terminal for each transaction.
	Password *string
	Out      io.Writer // gnokey's output is echoed here; nil for nowhere
}

func (g gnokeyCLI) args(fn string, args []string) []string {
	a := []string{"maketx", "call", "-pkgpath", g.Realm, "-func", fn}
	for _, x := range args {
		a = append(a, "-args", x)
	}
	a = append(a, "-gas-fee", g.GasFee, "-gas-wanted", g.GasWanted)
	if g.MaxDeposit != "" {
		a = append(a, "-max-deposit", g.MaxDeposit)
	}
	a = append(a, "-broadcast", "-chainid", g.ChainID, "-remote", g.Remote)
	if g.Home != "" {
		a = append(a, "-home", g.Home)
	}
	if g.Password != nil {
		a = append(a, "-insecure-password-stdin")
	}
	return append(a, g.Key)
}

// Call runs one gnokey maketx call and returns the realm's message when it
// fails.
func (g gnokeyCLI) Call(fn string, args ...string) error {
	cmd := exec.Command(g.Bin, g.args(fn, args)...)
	var buf bytes.Buffer
	var out io.Writer = &buf
	if g.Out != nil {
		out = io.MultiWriter(g.Out, &buf)
	}
	cmd.Stdout, cmd.Stderr = out, out
	if g.Password != nil {
		cmd.Stdin = strings.NewReader(*g.Password + "\n")
	} else {
		cmd.Stdin = os.Stdin
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gnokey %s: %w: %s", fn, err, gnokeyError(buf.String()))
	}
	return nil
}

// gnokeyError pulls the realm's panic message out of gnokey's output: the
// "Data:" line after "--= Error =--", else the last line.
func gnokeyError(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "--= Error =--" && i+1 < len(lines) {
			return strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "Data: ")
		}
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
