# Contributing

Every score is computed from files in this repo. The Google Sheet is an output:
its tabs are locked, and `sheet push` overwrites them. To change a score,
change a file here in a pull request.

## Changing a score

1. Edit the input: an arena CSV under `data/arenas/`, `data/candidates.csv`,
   `data/github-links.csv` or `data/github/evidence.json`.
2. Run `./bin/scoresheet score`. It rebuilds `data/scorecard.json`, re-embeds
   the page payload in `web/validator-scorecard.html` and regenerates
   `scoreboard/`. Use `-dry` first to see how many scores change.
3. Run the checks: `go build ./...`, `go vet ./...`, `gofmt -l .`,
   `go test ./...`.
4. Commit the input together with `data/scorecard.json`, `scoreboard/` and the
   page, and open a pull request.
5. Once merged, publish to the sheet. `sheet push -dry` lists every cell
   that differs between the sheet and the repo, and refuses while
   `scorecard.json` is stale. The sheet still holds the previous push, so the
   list should be exactly your change. Then run `sheet push`: no `-force` is
   needed while the sheet holds what the last push wrote. If push refuses
   because a tab was edited since the last push, see below. `-force` is only
   for the very first push and for discarding a sheet edit.

If someone edited the sheet and the edit should stay, run `freeze` to copy the
tabs back to `data/`, then follow the steps above with that diff. Once it is
merged the sheet matches the repo again, so `sheet push` needs no `-force`.

## Joining by moniker

Every join — candidates to arenas, arenas to GitHub links and evidence — goes
through `scoring.Key(moniker)`: trimmed and lower-cased. **Never join by
address.** Validators rotate keys between chains and between runs; only 17 of
86 sapphire addresses survived a single refresh. Address columns in the CSVs
are provenance only.

A candidate whose moniker is absent from an arena's CSV scores 0 in that
arena. That is deliberate: do not add a fallback to an older value.

## Adding an arena

An arena is one entry in `config/arenas.json` plus one file,
`data/arenas/<id>.csv`. Nothing else names the arenas: the divisor is the
number of entries with `in_total: true`, and the page, the scoreboard, Bilan's
columns and the Dashboard all follow the config.

1. Write `data/arenas/<id>.csv`: a header row, then one row per validator,
   with a moniker column and a score out of 100. For a gnomonitoring chain,
   set `chain` in the config and let `fetch -chain <chain>` write the file.
2. Add the entry to `config/arenas.json`. The order of the entries is the order
   of the columns in Bilan and of the sections on the page.
3. Run `score`, the checks, commit, and `sheet push` as above. `sheet push`
   creates the arena's tab.
4. Once the arena's results are final, add its tab to `scoring.FrozenTabs`
   so `fetch` and `firedrill` refuse to rewrite it without `-force`.

An arena with `in_total: true` changes every total, since each arena's weight
becomes 1 / (number of arenas in the total). Expect the ranking to move.

### Config fields

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | Names the CSV (`data/arenas/<id>.csv`) and the page section. Unique. `overview`, `profile` and `github` are taken by the page. `mainnet` is special: that entry is shown on its own section and never scored. |
| `label` | yes | Shown as the Bilan column header, on the page and on the scoreboard. |
| `source` | no | Provenance, shown on the scoreboard. |
| `tab` | yes | The sheet tab `sheet push` writes and `freeze` reads. Unique, and neither `Bilan` nor `Dashboard`. |
| `moniker_column` | yes | The CSV header holding the moniker. |
| `score_column` | yes | The CSV header holding the score. Headers differ between arenas because each was imported its own way. |
| `colour` | no | Hex colour for the page and the Dashboard. |
| `in_total` | no | `true` if the arena counts toward the total. At least one arena must. |
| `chain` | no | The gnomonitoring chain id `fetch` reads. Empty for an arena that is not an API chain. Unique. |
| `detail` | no | How the scorecard reads the rest of the CSV: `challenge`, `firedrill` or `onchain`. Empty means the arena publishes its score alone. |

`detail` reads columns by position, not by header, so a CSV with a detail kind
must follow the layout of the existing one:

- `onchain`: moniker, address, then `score`, `tier`, `sign`, `vp`,
  `proposer`, `critical`, `warning`, `freq`, `freq_rate`, `downtime`,
  `missed` — the layout `fetch` writes.
- `challenge`: Candidate, Moniker, Weighted, Simple, Penalty, Bonus, Final (w),
  Score — as in `data/arenas/challenge.csv`.
- `firedrill`: as `firedrill` writes it in `data/arenas/firedrill.csv`.

`LoadConfig` in `internal/arena/arena.go` checks these rules and refuses a
config that breaks one.

## Personal data

The repo is public. `data/candidates.csv`, `data/github-evidence.csv` and
`data/github/evidence.json` hold Discord handles, real names, bios and blogs;
each fact is public on its own. Keep personal e-mail addresses out of every
note. The HTML page embeds only the allowlisted fields of
`cmd/scoresheet/page.go`; widen it only on purpose.
