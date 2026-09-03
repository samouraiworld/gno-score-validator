# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Read `README.md` too — it carries the scoring rule and the fr_FR locale traps in
full. This file is what the README does not say: which spreadsheet, in what
order to run things, and which decisions are already settled.

## The spreadsheet

`Score Gno-Validator`, id `1HqBaQQsp9jLpJ4-LjGWgYUe7GEoGmgBPTwS4UjCL2qA`
(owner `lours@samourai.coop`). It is deliberately not hardcoded — every command
takes `-id`. Tabs: `Dashboard`, `Bilan`, `Test13-challenge`, `Score-test13`,
`Score-topaz`, `Score-sapphire`, `Github`.

`service-account.json` is a **symlink** to `../gno-onboarding-bot/service-account.json`
and is gitignored. The sheet must be shared with that account's `client_email`
as an Editor.

## Commands

```bash
go build -o bin/scoresheet ./cmd/scoresheet
ID=1HqBaQQsp9jLpJ4-LjGWgYUe7GEoGmgBPTwS4UjCL2qA

./bin/scoresheet -id $ID audit                 # numbers stored as text (read-only)
./bin/scoresheet -id $ID dump "Bilan!A1:K3"    # inspect cells      (read-only)
./bin/scoresheet -id $ID refresh -dry          # network tab from the live API
./bin/scoresheet -id $ID dashboard             # rebuild the Dashboard tab
./bin/scoresheet -id $ID snapshot -dry         # scorecard.json + re-embed in the page
```

There are no tests. Verification is `go build`, `go vet ./...`, `gofmt -l .`,
then `-dry` on every writing command before running it for real.

**Run order after changing any network tab:** `refresh` → `dashboard` → `snapshot`.
`bilan` only needs re-running when `data/github-links.csv` changes.

To eyeball the page, screenshot it rather than assuming — this is how the stale
hardcoded footer date was caught:

```bash
~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome --headless --no-sandbox \
  --window-size=1920,1200 --screenshot=/tmp/p.png --virtual-time-budget=3000 \
  "file://$PWD/web/validator-scorecard.html"
```

## Architecture

**Bilan is the hub, and it is mostly formulas.** Columns C–F pull each arena
with `=IFERROR(VLOOKUP(A2;'Score-sapphire'!A:C;3;FALSE);0)` and column I is
`=MIN(100; (C+D+E+F)/4 + H/10)`. So rewriting a network tab makes Bilan and the
Dashboard recompute **on their own** — no command needed. Only
`data/scorecard.json` and the HTML page are static and must be regenerated with
`snapshot`.

**The join is by moniker, case-insensitively — never by address.** Validators
rotate addresses between chains and between runs: only 17 of 86 sapphire
addresses survived one refresh. Any code keyed on the address column will
silently drop most rows.

**`snapshot` merges, it does not regenerate.** The `github` blocks on the 12
linked validators carry an all-time versus since-2025 contribution split that
exists in neither the sheet nor the CSVs. They are passed through as
`json.RawMessage`. Rebuilding the file from scratch would empty the page's
GitHub evidence view without any error.

**Columns A–G of Bilan are human-curated and must never be written.** The tool
writes only H, I, J, K.

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

## Chains

Only **`sapphire-1`** and **`pearl-1`** are served by
`https://gnomonitoring.samourai.live/api/reports/validators?chain=<id>`.
`topaz-1` returns just the two remaining core validators, and `test13` is a
`400 invalid chain ID` — so the frozen CSVs under `gno-monitoring-metrics/` are
the only surviving record of those two chains. Never point `refresh` at
`topaz-1`: it would wipe 85 rows down to 2.

## Next task: add pearl, and the fire drill

Verified as of 2026-08-25: `pearl-1` returns **79 validators**, same schema as
sapphire, **74 monikers** matching the scorecard (the 5 others are
`gno-core-validator-1/2/3`, `onbloc-val-01`, `samourai-crew-1` — team nodes, not
candidates). `refresh -chain pearl-1 -tab Score-pearl` already works once the
tab exists.

The fire drill is **not** an API chain (`fire-drill`, `fire-drill-test` all
return 400). It lives in the sibling repo `../validator-diagnostics` — a
submission portal with a `scoring/` package and `submissions.jsonl`; see its
`test-drill-2026-09-02.md` and `prd.md`. Bilan already has an empty `Fire-Drill`
column G reserved for it.

**The divisor is the trap.** Going from four arenas to five or six means
`ArenaDivisor` must change with the arena count, or totals run past 100. It is
hardcoded or assumed in all of these:

| Where | What |
| --- | --- |
| `internal/scoring/scoring.go` | `ArenaDivisor = 4`, `Networks`, `ScoreFormula`, a new `Col*` letter |
| `cmd/scoresheet/snapshot.go` | `total()`, `snapComponents` |
| `cmd/scoresheet/coerce.go` | `coerceTargets` indexes `Networks[0..2]` |
| `cmd/scoresheet/dashboard.go` | the component list and its colour, `cSapphire`-style |
| `web/validator-scorecard.html` | `SERIES` (arenas are `slice(0,4)`), the `0.25` multipliers at the profile and net cards, the formula line in the static markup, and a nav button + page for the new arena |

Bilan's column I formula is generated, so it follows `ScoreFormula`. But adding
a column shifts G–K, and `snapshot` reads Bilan by fixed index — both need
updating together.
