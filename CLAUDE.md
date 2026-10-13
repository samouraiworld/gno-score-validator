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

./bin/scoresheet score -dry                     # scorecard.json + page + scoreboard/, offline
./bin/scoresheet render                        # scoreboard/ + page payload, offline
./bin/scoresheet fetch -chain gnoland-1 -dry   # live report -> data/arenas/mainnet.csv, offline
./bin/scoresheet -id $ID sheet diff            # sheet vs repo, cell by cell (read-only)
./bin/scoresheet -id $ID sheet push -dry       # repo -> Bilan, arena tabs, Dashboard, lock
./bin/scoresheet -id $ID sheet lock -dry       # protect every tab (service account only)
./bin/scoresheet -id $ID freeze -dry           # arena tabs -> data/arenas/*.csv (import an edit)
./bin/scoresheet -id $ID audit                 # numbers stored as text (read-only)
./bin/scoresheet -id $ID dump "Bilan!A1:L3"    # inspect cells      (read-only)
```

`snapshot`, `bilan`, `refresh`, `coerce` and `dashboard` are retired: each
prints what replaces it.

Verification is `go build`, `go vet ./...`, `gofmt -l .`, `go test ./...`,
then `-dry` on every writing command before running it for real (`export`
ignores `-dry` and always saves its local PDF).

**Every arena is final** (challenge, test13, topaz, sapphire, pearl, fire
drill): `fetch` and `firedrill` refuse them without `-force`
(`scoring.FrozenTabs`). Only mainnet `gnoland-1` is expected to move. It is
**shown, not scored** (owner decision 2026-10-12): on that date its 6
validators were all team nodes, none of the 98 candidates, so a 7th arena would
only have scaled every score down by 1/7. Its weight gets decided when a
candidate first joins it. Its `config/arenas.json` entry (id `mainnet`,
`in_total: false`, `chain: gnoland-1`) is taken out of the arena list, so it
has no component and no weight. `fetch -chain gnoland-1` (no `-id`,
`current_year` like the testnets) writes `data/arenas/mainnet.csv`, and `score`
builds the `mainnet` block of `data/scorecard.json` from that file; no total
reads it. `fetch` picks its CSV by the `chain` field of config and guards it:
frozen arenas and a report losing a fifth of the file need `-force`.

**Run order after changing any CSV** (an arena, `candidates.csv`,
`github-links.csv`, `evidence.json`): `score` (which also renders
`scoreboard/`), commit `scoreboard/` and the page with `data/scorecard.json`,
then `sheet push -dry` and `sheet push -force`. `sheet push` refuses to run
while `scorecard.json` is stale, and while `sheet diff` finds anything. The
diff cannot tell a repo change from a sheet edit, so `-dry` lists the change
being published too: force only when every listed cell is one the repo
changed. To update mainnet: `fetch -chain gnoland-1 -dry`, then without
`-dry`, then `score`.

To eyeball the page, screenshot it rather than assuming — this is how the stale
hardcoded footer date was caught:

```bash
~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome --headless --no-sandbox \
  --window-size=1920,1200 --screenshot=/tmp/p.png --virtual-time-budget=3000 \
  "file://$PWD/web/validator-scorecard.html"
```

## Architecture

**The repo is the source of truth; the sheet is an output.** Every score is
computed in Go from `config/arenas.json` and the CSVs under `data/`. `sheet
push` writes Bilan and every arena tab as values (JSON numbers under RAW,
never text, never formulas), rebuilds the Dashboard (formulas over Bilan, so
its dropdown and charts stay live), leaves a note on A1 of each tab it writes
and protects every tab so only the service account can edit it. The file
owner can always edit regardless; `sheet push` runs `sheet diff` first and
refuses to overwrite a difference without `-force`. To keep a sheet edit,
`freeze` copies the tabs back to the CSVs for a pull request.

**The join is by moniker, case-insensitively — never by address.** Validators
rotate addresses between chains and between runs: only 17 of 86 sapphire
addresses survived one refresh. Any code keyed on the address column will
silently drop most rows.

**The `github` blocks come from `data/github/evidence.json`.** The 12 linked
validators carry an all-time versus since-2025 contribution split that exists
in neither the sheet nor the CSVs. `score` copies each block as
`json.RawMessage`, keyed by moniker, and refuses a block whose moniker matches
no candidate. Edit that file, not the blocks in `scorecard.json`, which
`score` overwrites. Only `source` and the github weights are still
carried over from the previous `scorecard.json`.

**Bilan is fully generated** (owner decision 2026-10-13, replacing "columns
A–H are human-curated"). Its layout follows the config: moniker and Discord
from `data/candidates.csv`, one column per arena in config order, then GitHub,
Score, GitHub login and confidence. With six arenas that is A–L as before; a
seventh shifts the last four right. Every cell is a value. Change a cell by
editing its CSV in a pull request, never in the sheet.

## Settled decisions — do not "fix" these

- A moniker absent from the live report scores **0** for that network. This was
  chosen knowingly, over carrying the last snapshot forward, when it cost three
  candidates ~20 points each. Do not add a last-known-value fallback.
- GitHub contributions count only from **2025** on. One candidate is affected
  (Santala-Research: 72 all-time, 0 after the cutoff). The page shows that gap
  as its "vs uncorrected" delta, which is why `score_current` and
  `score_corrected` both exist.
- `github-evidence.csv` holds real names, blogs, bios and Discord handles for 98
  people. The repo is public (owner decision 2026-10-13): each of those facts is
  public on its own, but keep personal e-mail addresses out of the notes.
- The HTML page embeds `publicPayload`, never `scorecard.json` as is: Discord
  handles and the github `name`, `bio`, `blog`, `note`, `evidence` fields stay
  out. Widen the allowlist in `cmd/scoresheet/page.go` only on purpose.

## Chains

Only **`sapphire-1`** and **`pearl-1`** are served by
`https://gnomonitoring.samourai.live/api/reports/validators?chain=<id>`.
`topaz-1` returns just the two remaining core validators, and `test13` is a
`400 invalid chain ID` — so the frozen CSVs under `gno-monitoring-metrics/` are
the only surviving record of those two chains. `fetch` refuses `topaz-1` (no
config arena has that chain): its report would wipe 85 rows down to 2. The
frozen and shrink guards refuse it too.

## Live pearl chain and the fire drill

Verified as of 2026-09-03: `pearl-1` returns **79 validators**, same schema as
sapphire, **74 monikers** matching the scorecard (the 5 others are
`gno-core-validator-1/2/3`, `onbloc-val-01`, `samourai-crew-1` — team nodes, not
candidates). It is an equal-weight arena; a candidate absent from its
report scores zero there, with no last-known-value fallback.

The fire drill is **not** an API chain. Its 2026-09-02 results are frozen in
`data/arenas/firedrill.csv` (rebuilt by `firedrill` from the portal exports
`scores.json` and `submissions.jsonl`) and join to the scorecard by `moniker`.
`portal_moniker` preserves spellings altered by the portal's filename rules,
while the exercise addresses are provenance only: none matches Score-pearl
because validators rotated keys between exercises.

**The divisor follows the config.** It is the number of `in_total: true`
arenas in `config/arenas.json`; nothing else hardcodes it. Adding an arena is
one config entry plus `data/arenas/<id>.csv` — see `CONTRIBUTING.md`. The page,
the scoreboard, Bilan's columns and the Dashboard all follow, and every total
shifts by the new weight.
