# Working with reports

A report is trendHERO's audience/engagement analysis of one Instagram account.
The `th-cli report` commands return the report blob as JSON; this file explains how
to read it and how to get a usable one.

## Where the status lives: `report.status`

The report response is a large, free-form object with several sibling sections
(`preview`, `report`, `saves_shares_report`, `openai_report`, `cache`, `type`,
`demo`, …). The status you care about is **nested at `report.status`** — there
is **no** top-level `status` field. Read it like:

```bash
th-cli report get <username> | jq -r '.report.status'
```

## Status meanings

| `report.status`      | Meaning                                 | Polling |
|----------------------|-----------------------------------------|---------|
| `ready`              | complete and usable                     | stop (terminal) |
| `recollecting`       | usable; a refresh is running            | stop (terminal) |
| `impossibleButReady` | usable; full collection wasn't possible | stop (terminal) |
| `impossible`         | failed — private/too small/unavailable  | stop (terminal, **failed**) |
| `collecting`         | still generating                        | keep polling |

The first three are all "ready" to the backend. `impossible` is terminal too,
but the report is unusable — there is nothing more to wait for. Only
`collecting` (and any initial/pending state) means "come back later".

## The order → wait → get workflow

1. **Try to fetch first.** `th-cli report get <username> --wait` returns the report
   if one already exists, polling past any `collecting` state to a terminal one.
2. **If it 404s** (exit `4`), no report has ever been ordered. Order one:
   `th-cli report order <username> --confirm --wait`. This **spends credits**, then
   polls until terminal and prints the final report.
3. **Always check `report.status`** in the output. Because `--wait` exits `0` at
   *any* terminal status, a `0` exit does **not** mean "good data" — an
   `impossible` report also exits `0`. Branch on `report.status`, not just the
   exit code.

### `--wait` tuning

- `--timeout` (default `5m`) — overall budget; exceeding it exits `5`
  (network/timeout class) with the partial state left server-side.
- `--interval` (default `10s`) — gap between polls.

Reports can take minutes to collect. If `--wait` times out, the report is
usually still generating — re-run `report get --wait` later; it does not
re-order or re-charge.

## Exit codes for report commands

| Code | When |
|------|------|
| 2 | missing/invalid token (set `TRENDHERO_TOKEN`) |
| 3 | 403 — token valid but not permitted (subscription/feature/balance) |
| 4 | 404 — no report for that username (order one) |
| 5 | network error or `--wait` timeout |
| 6 | 422 — cannot order: `not_enough_balance`, `impossible_report`, or an unconfirmed user. The API message is surfaced in the `error` field — read it |
| 7 | 503 — service temporarily unavailable; retry shortly |

## Note on `top-profiles`

`top-profiles` is a different surface: unauthenticated, no report status, no
polling. It just returns the ranked list. It never needs a token (one is sent
only if configured). See `examples.md`.
