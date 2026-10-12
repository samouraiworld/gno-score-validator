# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Read `README.md` too — it carries the scoring rule and the fr_FR locale traps in
full. This file is what the README does not say: which spreadsheet, in what
order to run things, and which decisions are already settled.

## The spreadsheet

`Score Gno-Validator`, id `1HqBaQQsp9jLpJ4-LjGWgYUe7GEoGmgBPTwS4UjCL2qA`
(owner `lours@samourai.coop`). It is deliberately not hardcoded — every command
takes `-id`. Tabs: `Dashboard`, `Bilan`, `Test13-challenge`, `Score-test13`,
`Score-topaz`, `Score-sapphire`, `Score-pearl`, `Score-firedrill`, `Github`.

`service-account.json` is a **symlink** to `../gno-onboarding-bot/service-account.json`
and is gitignored. The sheet must be shared with that account's `client_email`
as an Editor.

## Commands

```bash
go build -o bin/scoresheet ./cmd/scoresheet
ID=1HqBaQQsp9jLpJ4-LjGWgYUe7GEoGmgBPTwS4UjCL2qA

./bin/scoresheet -id $ID audit                 # numbers stored as text (read-only)
./bin/scoresheet -id $ID dump "Bilan!A1:L3"    # inspect cells      (read-only)
./bin/scoresheet -id $ID dashboard             # rebuild the Dashboard tab
./bin/scoresheet -id $ID freeze -dry           # final tabs -> data/frozen/*.csv
./bin/scoresheet -id $ID snapshot -dry         # scorecard.json + page + scoreboard/
./bin/scoresheet render                        # scoreboard/ + page payload, offline, no -id
./bin/scoresheet mainnet -dry                  # gnoland-1, shown not scored, no -id
```

Verification is `go build`, `go vet ./...`, `gofmt -l .`, `go test ./...`,
then `-dry` on every writing command before running it for real.

**Every arena is final** (challenge, test13, topaz, sapphire, pearl, fire
drill): `refresh` and `firedrill` refuse their tabs without `-force`
(`scoring.FrozenTabs`). Only mainnet `gnoland-1` is expected to move. It is
**shown, not scored** (owner decision 2026-10-12): on that date its 6
validators were all team nodes, none of the 98 candidates, so a 7th arena would
only have scaled every score down by 1/7. Its weight gets decided when a
candidate first joins it. `mainnet` (no `-id`, `current_year` like the
testnets) fetches it into the `mainnet` block of `data/scorecard.json`, which
`snapshot` carries across like the github blocks; no total reads it.

**Run order after changing any tab:** `dashboard` → `snapshot` (which also
renders `scoreboard/`). Commit `scoreboard/` and the page with
`data/scorecard.json`. To update mainnet: `mainnet -dry`, then `mainnet`.
`bilan` only needs re-running when `data/github-links.csv` or the scoring formula
changes.

To eyeball the page, screenshot it rather than assuming — this is how the stale
hardcoded footer date was caught:

```bash
~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome --headless --no-sandbox \
  --window-size=1920,1200 --screenshot=/tmp/p.png --virtual-time-budget=3000 \
  "file://$PWD/web/validator-scorecard.html"
```

## Architecture

**Bilan is the hub, and it is mostly formulas.** Columns C–G pull each arena
with `=IFERROR(VLOOKUP(A2;'Score-pearl'!A:C;3;FALSE);0)` and column J is
`=MIN(100; (C+D+E+F+G+H)/6 + I/10)`. So rewriting a network tab makes Bilan and
the Dashboard recompute **on their own** — no command needed. Only
`data/scorecard.json` and the HTML page are static and must be regenerated with
`snapshot`.

**The join is by moniker, case-insensitively — never by address.** Validators
rotate addresses between chains and between runs: only 17 of 86 sapphire
addresses survived one refresh. Any code keyed on the address column will
silently drop most rows.

**The `github` blocks come from `data/github/evidence.json`.** The 12 linked
validators carry an all-time versus since-2025 contribution split that exists
in neither the sheet nor the CSVs. `snapshot` copies each block as
`json.RawMessage`, keyed by moniker, and refuses a block whose moniker matches
no Bilan row. Edit that file, not the blocks in `scorecard.json`, which
`snapshot` overwrites. The weights and the `mainnet` block are still carried
over from the previous `scorecard.json`.

**Columns A–H of Bilan are human-curated and must never be written.** The tool
writes only I, J, K, L.

## Settled decisions — do not "fix" these

- A moniker absent from the live report scores **0** for that network. This was
  chosen knowingly, over carrying the last snapshot forward, when it cost three
  candidates ~20 points each. Do not add a last-known-value fallback.
- GitHub contributions count only from **2025** on. One candidate is affected
  (Santala-Research: 72 all-time, 0 after the cutoff). The page shows that gap
  as its "vs uncorrected" delta, which is why `score_current` and
  `score_corrected` both exist.
- `github-evidence.csv` holds real names, blogs, bios and Discord handles for 98
  people. The GitHub repo is private and must stay private — for that reason,
  not because of any key.
- The HTML page embeds `publicPayload`, never `scorecard.json` as is: Discord
  handles and the github `name`, `bio`, `blog`, `note`, `evidence` fields stay
  out. Widen the allowlist in `cmd/scoresheet/page.go` only on purpose.

## Chains

Only **`sapphire-1`** and **`pearl-1`** are served by
`https://gnomonitoring.samourai.live/api/reports/validators?chain=<id>`.
`topaz-1` returns just the two remaining core validators, and `test13` is a
`400 invalid chain ID` — so the frozen CSVs under `gno-monitoring-metrics/` are
the only surviving record of those two chains. Never point `refresh` at
`topaz-1`: it would wipe 85 rows down to 2. The frozen-tab and shrink guards
now refuse that too.

## Live pearl chain and the fire drill

Verified as of 2026-09-03: `pearl-1` returns **79 validators**, same schema as
sapphire, **74 monikers** matching the scorecard (the 5 others are
`gno-core-validator-1/2/3`, `onbloc-val-01`, `samourai-crew-1` — team nodes, not
candidates). It is an equal-weight arena; a candidate absent from its
report scores zero there, with no last-known-value fallback.

The fire drill is **not** an API chain. Its 2026-09-02 results are frozen in
`data/fire-drill-2026-09-02.csv` and join to the scorecard by `moniker`.
`portal_moniker` preserves spellings altered by the portal's filename rules,
while the exercise addresses are provenance only: none matches Score-pearl
because validators rotated keys between exercises.

**The divisor is the trap.** The fire drill creates six arenas, so
`ArenaDivisor` is 6 or totals run past 100. The places that must move together
are:

| Where | What |
| --- | --- |
| `internal/scoring/scoring.go` | `ArenaDivisor = 6`, `ScoreFormula`, and `ColFireDrill = "H"` |
| `cmd/scoresheet/snapshot.go` | `total()`, `snapComponents` |
| `cmd/scoresheet/dashboard.go` | the component list and its colour |
| `web/validator-scorecard.html` | `SERIES`, `ARENA_WEIGHT`, the formula line in the static markup, and a nav button + page for the new arena |

Bilan's column J formula is generated, so it follows `ScoreFormula`. Filling the
reserved fire-drill column does not shift later columns, but `snapshot` reads
Bilan by fixed index and must gain the new component explicitly.
