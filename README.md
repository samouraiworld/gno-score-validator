# score-validator

Scoring for gno.land test13 validator candidates: a Go tool that computes and
formats the **Score Gno-Validator** Google Sheet, and a standalone HTML
scorecard built from the same numbers.

## The score

```
Score = ( Test13-challenge + Score-test13 + Score-topaz + Score-sapphire ) / 4
      + GitHub / 10,   capped at 100
```

The four arenas carry equal weight, so each contributes at most 25 points.
GitHub is a bonus on top worth at most 10: it can lift a candidate, never sink
one. **The cap matters** — without it the formula reaches 110.

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
| `cmd/scoresheet` | the CLI: audit, dump, coerce, bilan, dashboard, export |
| `internal/scoring` | the scoring rule and the Bilan schema, shared by every command |
| `internal/sheetx` | Sheets API helpers, and the locale rules that break formulas |
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
./bin/scoresheet -id $ID dump "Bilan!A1:K3" # inspect cells    (read-only)
./bin/scoresheet -id $ID coerce -dry        # plan the text→number fix
./bin/scoresheet -id $ID bilan -dry         # plan the Score rewrite
./bin/scoresheet -id $ID dashboard          # rebuild the Dashboard tab
./bin/scoresheet -id $ID export Dashboard out.pdf
```

Every writing command takes `-dry`. Start there — `bilan` rewrites a column
that a human curates.

`dashboard` is idempotent: it clears the tab's cells, charts, banding and
conditional rules before rebuilding, so repeated runs do not stack duplicates.

## What the tool touches

`bilan` writes **only** columns H, I, J and K:

- **H** GitHub participation score, restricted to the cutoff year
- **I** the total, as a capped formula rather than a pasted number
- **J**, **K** GitHub login and match confidence, so the bonus is auditable
  from the same row instead of taken on trust

The human columns **A–G are never written**.

`dashboard` builds its own tab and reads everything else through formulas, so
the standings stay live: edit Bilan and the table reorders itself.

## fr_FR, and three ways it breaks formulas

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

Weights are written as fractions (`/4`, `/10`) rather than `0.25` and `0.1`, so
the decimal separator never enters a formula at all.

A related trap, fixed once by `coerce`: values imported with a `.` decimal
separator, and bonuses written `"+0.50"`, were stored as **text**. `SUM` and
`AVERAGE` skip text silently, so those columns averaged to zero without any
warning. `audit` reports the condition; `coerce` writes the values back as JSON
numbers, which sidesteps locale parsing entirely.

## The HTML scorecard

`web/validator-scorecard.html` is self-contained — open it in a browser, no
server and no network. It carries the same snapshot the sheet does: overall
standings, a per-validator profile with the on-chain detail for all three
networks, one page per scoring arena, and the GitHub evidence.

It rounds half away from zero to match Google Sheets. Plain `toFixed` rounds on
the binary representation, which renders the median as 59.42 against the
sheet's 59,43.
