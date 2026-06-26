# Recipes

Copy-paste examples. They assume `th-cli` is on PATH and (for reports) that
`TRENDHERO_TOKEN` is set. The CLI uses the trendHERO API at
`https://trendhero.io`, so no host configuration is needed:

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
