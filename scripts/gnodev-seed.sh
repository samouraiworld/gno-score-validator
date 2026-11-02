#!/usr/bin/env bash
# Registers four test valopers on a running gnodev, for the scoresheet publish
# end-to-end test (cmd/scoresheet/publish_e2e_test.go) and for a manual run
# with Memba. Keys are derived from gnodev's public test1 mnemonic at HD
# indices 1-4 in a throwaway keybase: nothing here is a secret, and none of it
# is worth anything on a real chain.
#
# gnodev, gnokey and gnoland must be on PATH and built from the GNOROOT checkout,
# at the gno commit gnoland-1 runs (MAINNET_GNO_REF in samcrew-deployer's
# ci/mainnet-gno-refs.env):
#
#   scripts/gnodev-seed.sh                                  # gnodev on 127.0.0.1:26657
#   REMOTE=127.0.0.1:36657 SEED_HOME=/tmp/k scripts/gnodev-seed.sh
#
# Prints one "moniker,operator_address" line per valoper. Safe to run twice:
# a valoper already registered is skipped.
set -euo pipefail

: "${GNOROOT:?set GNOROOT to the gno checkout gnodev, gnokey and gnoland were built from}"
export GNOROOT
REMOTE="${REMOTE:-127.0.0.1:26657}"
CHAIN_ID="${CHAIN_ID:-dev}"
SEED_HOME="${SEED_HOME:-$(mktemp -d)}"
REALM="gno.land/r/gnops/valopers"
TX=(-gas-fee 1000000ugnot -gas-wanted 50000000 -broadcast -chainid "$CHAIN_ID" -remote "$REMOTE" -insecure-password-stdin -quiet)
# gnodev's default account, test1, funded at genesis (and the publisher of the
# staged validator_scores copy). This mnemonic is public in the gno repo
# (gno.land/pkg/integration/node_testing.go, DefaultAccount_Seed).
TEST1_MNEMONIC="source bonus chronic canvas draft south burst lottery vacant surface solve popular case indicate oppose farm nothing bullet exhibit title speed wink action roast"
MONIKERS=("Seed Alpha" "seed-bravo" "Seed_Charlie" "Seed Delta")

add_key() { # name hd-index
  printf '%s\n\n' "$TEST1_MNEMONIC" | gnokey add -home "$SEED_HOME" -recover \
    -insecure-password-stdin -index "$2" -force -quiet "$1" >/dev/null
}

key_addr() { # name
  gnokey list -home "$SEED_HOME" | sed -n "s/^[0-9]*\. $1 (local) - addr: \(g1[a-z0-9]*\) .*/\1/p"
}

mkdir -p "$SEED_HOME"
add_key test1 0
for i in 1 2 3 4; do
  add_key "seed$i" "$i"
  addr="$(key_addr "seed$i")"
  moniker="${MONIKERS[$((i - 1))]}"
  current="$(gnokey query vm/qrender -data "$REALM:$addr" -remote "$REMOTE")"
  if [[ "$current" == *"unknown address"* ]]; then
    echo "" | gnokey maketx send -home "$SEED_HOME" -send 10000000ugnot -to "$addr" "${TX[@]}" test1 >/dev/null
    rm -rf "$SEED_HOME/secrets$i"
    gnoland secrets init -data-dir "$SEED_HOME/secrets$i" >/dev/null 2>&1
    pubkey="$(gnoland secrets get -data-dir "$SEED_HOME/secrets$i" validator_key.pub_key | tr -d '"')"
    echo "" | gnokey maketx call -home "$SEED_HOME" -pkgpath "$REALM" -func Register \
      -args "$moniker" -args "Test valoper seeded for scoresheet publish" -args cloud \
      -args "$addr" -args "$pubkey" "${TX[@]}" "seed$i" >/dev/null
  fi
  printf '%s,%s\n' "$moniker" "$addr"
done
