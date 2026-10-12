# score-validator

Scoring for gno.land test13 validator candidates: a Go tool that computes and
formats the **Score Gno-Validator** Google Sheet, and a standalone HTML
scorecard built from the same numbers.

## The score

```
Score = ( Test13-challenge + Score-test13 + Score-topaz + Score-sapphire + Score-pearl + Fire-drill ) / 6
      + GitHub / 10,   capped at 100
```

The six arenas carry equal weight, so each contributes at most 16.7 points.
GitHub is a bonus on top worth at most 10: it can lift a candidate, never sink
one. **The cap matters** — without it the formula reaches 110.
The snapshot records each arena's `1/6` weight as the full repeating decimal
available to JSON; the executable rule remains the integer divisor above.

GitHub participation is scored as:

| Signal | Weight |
| --- | --: |
| Merged pull request | 25 |
| Unmerged pull request | 8 |
| Issue opened | 5 |
| Commit | 3 |
| Comment thread | 2 |

A merged pull request is the only artefact that proves code landed, so it
outweighs intent and discussion by a wide margin. Only contributions dated
**2025 or later** count: earlier work is real, but says nothing about
involvement in the testnet being scored.

12 of the 98 candidates could be tied to a GitHub account, each verified by
hand — see `data/github-evidence.csv` for the evidence behind every match. A
blank GitHub column is absence of evidence, not evidence of absence, and never
costs points.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/scoresheet` | the CLI: audit, dump, coerce, bilan, refresh, snapshot, dashboard, export |
| `internal/scoring` | the scoring rule and the Bilan schema, shared by every command |
| `internal/sheetx` | Sheets API helpers, and the locale rules that break formulas |
| `internal/monitoring` | the gnomonitoring report client |
| `gno-monitoring-metrics/` | dated CSV snapshots of those reports, one per chain |
| `data/github-links.csv` | moniker → GitHub account, confidence, participation score |
| `data/github-evidence.csv` | the evidence behind each match, for review |
| `data/scorecard.json` | the merged snapshot the HTML page embeds |
| `web/validator-scorecard.html` | self-contained interactive scorecard |

## Usage

The spreadsheet must be shared with the service account as an **Editor**. Find
its address in the key file:

```bash
python3 -c "import json;print(json.load(open('service-account.json'))['client_email'])"
```

Then:

```bash
go build -o bin/scoresheet ./cmd/scoresheet

ID=<spreadsheet-id>
./bin/scoresheet -id $ID audit              # numbers stored as text (read-only)
./bin/scoresheet -id $ID dump "Bilan!A1:L3" # inspect cells    (read-only)
./bin/scoresheet -id $ID coerce -dry        # plan the text→number fix
./bin/scoresheet -id $ID bilan -dry         # plan the Score rewrite
./bin/scoresheet -id $ID refresh -dry       # plan a network tab refresh
./bin/scoresheet -id $ID snapshot -dry      # plan the scorecard.json rebuild
./bin/scoresheet -id $ID dashboard          # rebuild the Dashboard tab
./bin/scoresheet -id $ID export Dashboard out.pdf
```

Every writing command takes `-dry`. Start there — `bilan` rewrites the generated
score and GitHub columns.

`refresh` rewrites a network tab from the live gnomonitoring report. It
defaults to `-chain sapphire-1 -tab Score-sapphire -period current_year`, and
prints the moniker-level diff before writing anything.

`snapshot` mirrors the sheet into `data/scorecard.json` and re-embeds it in the
HTML page, so the two can never disagree about a score. It **merges rather than
regenerates**: every field the sheet holds is rewritten from the sheet, and the
`github` evidence blocks are carried over untouched. They have to be — the
page's all-time versus since-2025 contribution split exists in neither the sheet
nor the CSVs, and rebuilding the file from scratch would silently drop it.

`dashboard` is idempotent: it clears the tab's cells, charts, banding and
conditional rules before rebuilding, so repeated runs do not stack duplicates.

## What the tool touches

`bilan` writes **only** columns I, J, K and L:

- **I** GitHub participation score, restricted to the cutoff year
- **J** the total, as a capped formula rather than a pasted number
- **K**, **L** GitHub login and match confidence, so the bonus is auditable
  from the same row instead of taken on trust

The human columns **A–H are never written**.

`dashboard` builds its own tab and reads everything else through formulas, so
the standings stay live: edit Bilan and the table reorders itself.

## The monitoring reports

The metrics behind the four network tabs come from gnomonitoring. Only
**sapphire-1** and **pearl-1** are still served: `topaz-1` returns the two
remaining core validators and `test13` is a `400 invalid chain ID`, so the CSVs
under `gno-monitoring-metrics/` are frozen backups for those two chains and the
only record left of what they held.

Bilan reads a network tab with `VLOOKUP(A2;'Score-sapphire'!A:C;3;FALSE)` — by
**moniker**, not by address, and case-insensitively. That matters more than it
looks: between the 18/08 snapshot and the live report only 17 sapphire
addresses out of 86 survived, 24 monikers having rotated to a new address. A
join on the address column would have dropped three quarters of the rows.

A moniker absent from the live report falls through to the `IFERROR` zero. That
is a scoring decision, not an accident: leaving the active set costs the whole
sapphire contribution of 16.7 points.

## fr_FR, and four ways it breaks formulas

The spreadsheet's locale is French. Each of these produced a silent wrong
answer before it was pinned down, and all three are handled in
`internal/sheetx`:

1. **The argument separator is `;`, not `,`.** Formulas are parsed in the
   sheet's locale, so a comma-separated formula lands as `#ERROR!` on every
   row. Function names, however, may stay English — both `MEDIAN` and
   `MEDIANE` work.
2. **Array literals separate columns with `\`.** `SPARKLINE`'s options array is
   the usual victim.
3. **A tab name containing a hyphen must be quoted.** `Score-test13!E2` parses
   as the subtraction `Score` minus `test13!E2` and returns a plausible-looking
   wrong number rather than an error. Always go through `sheetx.Ref`.

4. **A `.` decimal can be read as a date.** `12.6` imported into the proposer
   column landed as the 12th of June, storing the serial 46185 where a
   percentage belonged — on 50 of 53 rows. `audit` cannot see it, because a
   date *is* a number. `refresh` sidesteps it by sending JSON numbers under
   `RAW` and resetting the column's number format, since a cell still carrying
   the date format would render a good value as a bad date.

Weights are written as fractions (`/6`, `/10`) rather than decimal multipliers, so
the decimal separator never enters a formula at all.

A related trap, fixed once by `coerce`: values imported with a `.` decimal
separator, and bonuses written `"+0.50"`, were stored as **text**. `SUM` and
`AVERAGE` skip text silently, so those columns averaged to zero without any
warning. `audit` reports the condition; `coerce` writes the values back as JSON
numbers, which sidesteps locale parsing entirely.

## The HTML scorecard

`web/validator-scorecard.html` is self-contained — open it in a browser, no
server and no network. It carries the same snapshot the sheet does: overall
standings, a per-validator profile with the on-chain detail for all four
networks, one page per scoring arena, and the GitHub evidence.

Run `snapshot` after any change to the sheet, or the page keeps serving the
previous run's numbers while the spreadsheet has moved on.

It rounds half away from zero to match Google Sheets. Plain `toFixed` rounds on
the binary representation, which renders the median as 59.42 against the
sheet's 59,43.
