# Recipes

Copy-paste examples. They assume `th-cli` is on PATH and (for `search` and the
report commands) that `TRENDHERO_TOKEN` is set. The CLI uses the trendHERO API
at `https://trendhero.io`, so no host configuration is needed:

```bash
export TRENDHERO_TOKEN=<AccessToken>
```

## Check the binary

```bash
th-cli version
# {"version":"1.2.0","commit":"abc1234"}
```

## Top profiles (no token)

Top US accounts for a given month, then extract usernames + follower counts:

```bash
th-cli top-profiles --country US --year 2026 --month 6
th-cli top-profiles --country US | jq -r '.[] | "\(.username)\t\(.follower_count)"'
```

Each item carries fields like `username`, `pk`, `country`, `full_name`,
`follower_count`, `following_count`, `general_er`, `media_count`,
`is_verified`, `profile_pic_url`. The token is sent only if configured, never
required. An invalid `--country` exits `1`; `--type relative` is not yet
implemented and exits `6` (read the `error` message).

## Find accounts (PAID — spends credits per result)

```bash
# Common filters. --size defaults to 15 and is a direct cost multiplier.
th-cli search --keywords fitness --country US --followers-min 50000 --er-min 3 --size 20

# Usernames + follower counts
th-cli search --keywords "sustainable fashion" --followers-min 25000 \
  | jq -r '.results[] | "\(.username)\t\(.follower_count)"'

# Full filter surface (premium audience filters, sort, city locations, ages)
cat <<'JSON' | th-cli search --filters-json - --size 25
{
  "audience_locations": [{ "type": "country", "country": "US", "gte": 50 }],
  "follower_count": { "gte": 50000 },
  "sort": [{ "general_er": "desc" }]
}
JSON
```

Three things to get right (details in `search.md`):

```bash
# Pages are 0-INDEXED — this is the SECOND page.
th-cli search --keywords travel --page 1 --size 50

# --size must be 1-50. Out of range fails locally, exit 1, no call, no charge.
th-cli search --size 5000
# {"error":"invalid --size 5000: must be between 1 and 50 ..."}   # exit 1

# A zero-match search is a SUCCESS (exit 0) with an empty array — read `results`.
th-cli search --keywords veryobscureniche | jq '.results | length'   # 0
```

Page only when you need to, and only inside `pagination.total_pages` (which is
capped by the plan and shrinks as credits are spent) — there is no `--all`:

```bash
th-cli search --keywords travel --size 50 > p0.json
jq '.pagination' p0.json   # {"page":0,"size":50,"total_pages":6,"total_results":48231}
```

## Fetch an existing report

```bash
# Quick fetch — may still be `collecting`.
th-cli report get cristiano

# Preferred: poll until terminal, then read the status.
th-cli report get cristiano --wait --timeout 10m --interval 15s
th-cli report get cristiano --wait | jq -r '.report.status'
```

Handle the outcome by exit code and status:

```bash
if th-cli report get "$user" --wait > report.json; then
  status=$(jq -r '.report.status' report.json)
  case "$status" in
    ready|recollecting|impossibleButReady) echo "usable report" ;;
    impossible)                            echo "account can't be analyzed" ;;
  esac
else
  code=$?   # 2 token · 3 forbidden · 4 not-ordered · 5 timeout · 6/7 API
  echo "report fetch failed (exit $code)"
fi
```

## Order a new report (PAID) and wait

Ordering spends credits, so it needs explicit opt-in. Only do this when the user
has asked for a fresh report.

```bash
# One-shot: order, then poll to terminal and print the final report.
th-cli report order someuser --confirm --wait

# Standing opt-in for a session instead of repeating --confirm:
export TRENDHERO_ALLOW_WRITES=1
th-cli report order someuser --wait
```

Without `--confirm` (and without `TRENDHERO_ALLOW_WRITES=1`) the command refuses
and makes **no** API call:

```bash
th-cli report order someuser
# {"error":"ordering a report for \"someuser\" spends account credits; re-run with --confirm ..."}
# exit code 1
```

If the account lacks credits the order exits `6` with the API message:

```bash
th-cli report order someuser --confirm
# {"error":"not_enough_balance", ...}   # exit 6
```

## Common pitfalls

- A `0` exit from `--wait` does **not** guarantee a usable report — an
  `impossible` status also exits `0`. Always inspect `report.status`.
- `report get` on a username that was never ordered returns `404` (exit `4`),
  not an empty report — order it first.
- The status is at `report.status`, not the top level.
- `search` pages are **0-indexed** and `--size` is capped at 50 (rejected, not
  clamped). A search matching nothing exits `0` with `"results": []`.
- A `search` hit with `"paid": true` already has a report you own — `report get`
  it rather than ordering a duplicate.
