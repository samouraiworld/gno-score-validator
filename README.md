# score-validator

Scoring for gno.land test13 validator candidates: a Go tool that computes and
formats the **Score Gno-Validator** Google Sheet, and a standalone HTML
scorecard built from the same numbers.

**Scores: [scoreboard/](scoreboard/README.md)** — the ranking and one page per
validator, generated from `data/scorecard.json` and readable right here on GitHub.

Every arena is final: the testnets (test13, topaz, sapphire, pearl) have ended
and the fire drill has closed. Mainnet (`gnoland-1`) is shown on the page and
the scoreboard but no total includes it: none of its validators is a candidate
yet, and its weight will be decided when one joins.

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
| `cmd/scoresheet` | the CLI: audit, dump, coerce, bilan, refresh, firedrill, freeze, snapshot, render, dashboard, export |
| `internal/scoring` | the scoring rule and the Bilan schema, shared by every command |
| `internal/sheetx` | Sheets API helpers, and the locale rules that break formulas |
| `internal/monitoring` | the gnomonitoring report client |
| `gno-monitoring-metrics/` | dated CSV snapshots of those reports, one per chain |
| `data/github-links.csv` | moniker → GitHub account, confidence, participation score |
| `data/github-evidence.csv` | the evidence behind each match, for review |
| `data/github/evidence.json` | the `github` block of each linked validator, with the all-time versus since-2025 split; `snapshot` copies it into the scorecard |
| `data/scorecard.json` | the merged snapshot the HTML page and the scoreboard are built from |
| `data/frozen/` | CSV copies of the final arena tabs and Bilan, written by `freeze` |
| `data/fire-drill-2026-09-02.csv` | the fire drill rubric, built by `firedrill` from the portal exports |
| `scores.json`, `submissions.jsonl` | the fire drill portal exports |
| `scoreboard/` | generated Markdown: the ranking and one page per validator |
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
./bin/scoresheet -id $ID refresh -dry -chain <id> -tab <tab>  # plan a network tab refresh
./bin/scoresheet -id $ID freeze -dry        # plan the copy of the final tabs to data/frozen/
./bin/scoresheet -id $ID snapshot -dry      # plan the scorecard.json rebuild
./bin/scoresheet render                     # regenerate scoreboard/ and the page payload (offline, no -id)
./bin/scoresheet mainnet -dry               # plan the gnoland-1 fetch (shown, not scored; no -id)
./bin/scoresheet -id $ID dashboard          # rebuild the Dashboard tab
./bin/scoresheet -id $ID export Dashboard out.pdf
```

Every writing command takes `-dry`. Start there — `bilan` rewrites the generated
score and GitHub columns.

`refresh` rewrites a network tab from the live gnomonitoring report. It needs
both `-chain` and `-tab`, and prints the moniker-level diff before writing
anything. It refuses an empty report, a report that would drop more than a
fifth of the tab, and any frozen arena tab, unless given `-force`. `firedrill`
refuses to rewrite the fire drill without `-force` too.

`freeze` copies Bilan and every final arena tab into `data/frozen/`, so the
scores live in git and not only in an editable spreadsheet.

`snapshot` mirrors the sheet into `data/scorecard.json` and re-embeds a public
copy of it in the HTML page, so the two can never disagree about a score. It **merges rather than
regenerates**: every field the sheet holds is rewritten from the sheet, the
`github` evidence blocks are copied untouched from `data/github/evidence.json`,
and the weights and the `mainnet` block are carried over from the existing
file. The evidence file is the only record of the page's all-time versus
since-2025 contribution split. `snapshot` stops if one of its monikers matches
no Bilan row, so a block cannot vanish from the page unnoticed.
It stops on a formula error or a number stored as text in Bilan, on a moniker
listed twice, and on a column J that disagrees with the scoring rule. Equal
scores share a rank. It ends by regenerating the scoreboard.

`render` builds `scoreboard/` from `data/scorecard.json` alone, without the
sheet or the key, and re-embeds the page's payload. Both carry public fields
only. The page keeps the GitHub login, profile link, match rule, confidence and
contribution counts, and drops Discord handles, real names, bios, blogs and the
matching notes. The page payload goes through an allowlist
(`cmd/scoresheet/page.go`), so a new evidence field stays private by default,
and a test fails if the committed page differs from it.

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
