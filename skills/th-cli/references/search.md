# Account search (`th-cli search`)

Discovery search over trendHERO's Instagram account index — the same filter
surface as the web UI's discovery search, exposed as `POST /api/public/v1/searches`.

> ⚠️ **`search` SPENDS CREDITS on every call** — a flat charge per search plus
> one charge **per result returned**. There is no free preview and no
> count-only mode. `--size` is a direct cost multiplier, and there is **no
> auto-paging flag**: every page is a separate, separately-billed call. Search
> credits come from the same pool the web UI spends, so scripting is never
> cheaper than clicking.

Unlike `report order`, `search` has **no `--confirm` guard**. A single search is
cheap and agents legitimately run many, so a prompt on every call would just
train you to pass it reflexively. The cost is documented instead — respect it.

## The three things that bite

1. **Pages are 0-indexed.** The first page is `--page 0` (the default).
   `--page 1` is the *second* page. There is no page "1" meaning "first".
2. **`--size` must be 1–50.** Out of range is rejected — locally by the CLI
   (exit `1`), and by the server with a **422, never a clamp**. Asking for
   `--size 5000` does not quietly give you 15; it gives you an error.
3. **The result set is capped by the plan, and the cap shrinks as you spend.**
   `pagination.total_results` is the raw match count and is often huge;
   `pagination.total_pages` is how many pages *you* may actually reach. Page
   through using `total_pages`, never `total_results / size`.

## Quick start

```bash
# First page, 15 results, US fitness accounts with 50k+ followers
th-cli search --keywords fitness --country US --followers-min 50000

# Second page (0-indexed!), 25 at a time
th-cli search --keywords fitness --country US --followers-min 50000 --page 1 --size 25
```

## Output shape

```json
{
  "results": [
    { "pk": 173560420, "username": "cristiano", "full_name": "Cristiano Ronaldo",
      "follower_count": 600000000, "general_er": 0.012, "country": "US",
      "paid": false, "payment_status": 0, "profile_pic_url": "https://…" }
  ],
  "pagination": { "page": 0, "size": 15, "total_pages": 20, "total_results": 48231 }
}
```

- `results` — matching accounts. **May be empty** (see below).
- `paid` / `payment_status` — whether your Space already owns a report for that
  account. When `paid` is true, `th-cli report get <username>` returns it
  without ordering (and without paying again).
- `pagination.page` / `.size` — echoed back exactly as requested.
- `pagination.total_pages` — `min(ceil(total_results / size), ceil(plan_cap / size))`.
- `pagination.total_results` — raw Elasticsearch match count; frequently far
  larger than `total_pages × size`.

Deliberately **not** in a search hit: `posts`, `note`, `status`, and the bulk
audience distributions `audience_by_city` / `audience_by_country`. The scalar
audience signals (`audience_authentic`, `audience_male`, `aqs`) *are* returned.
For full audience data, order a report.

### A zero-match search is a SUCCESS

A search that matches nothing returns **HTTP 200, exit 0**, with
`"results": []` and `"total_pages": 0`. It is not an error, and it still costs
the flat per-search charge (only the per-result charges are naturally zero).

**Read `results`, not the exit code**, to learn whether anything matched:

```bash
th-cli search --keywords veryobscureniche | jq '.results | length'
```

This matters because "nothing matched" and "you exceeded your plan cap" would
otherwise both look like exit `6`.

## Flags

| Flag | Maps to | Notes |
|------|---------|-------|
| `--keywords` | `keywords` | Phrases matched against username / full name / bio. Repeatable or comma-separated. **Any** may match (OR). |
| `--followers-min` / `--followers-max` | `follower_count` `{gte,lte}` | Either bound alone is fine. |
| `--er-min` / `--er-max` | `general_er` `{gte,lte}` | Engagement rate **in percent** (`--er-min 2.5` = 2.5%). |
| `--country` | `locations` (include half) | ISO code(s), repeatable or comma-separated. Where the **account** is, not its audience. |
| `--language` | `languages` | **3-letter** code(s) the account posts in: `spa`, `eng`, `cat`, `fre`… A 2-letter code (`es`, `en`) is rejected locally — it would match nothing and still bill. |
| `--category` | `instagram_category` | Instagram's own business-category labels. |
| `--gender` | `gender` | `male`, `female`, `none`, `brand` — translated to the numeric codes the index stores. |
| `--verified` | `is_verified` | `--verified` = verified only; `--verified=false` = **unverified only**; omit for either. |
| `--with-contacts` | `with_contacts` | `biography_contacts` (email/phone in the bio) and/or `trendhero_contacts` (a contact trendHERO holds). Repeatable; OR. |
| `--filters-json <file\|->` | the whole `search_params` object | The full surface. Merged **over** the flags above. |
| `--page` | `page` | **0-indexed.** Default `0`. |
| `--size` | `size` | 1–50. Default `15`. **Charged per result.** |

Global flags apply too: `--token`, `--base-url`, `--config`.

## `--filters-json` — the full filter surface

The flags cover the common cases. Everything else — premium audience filters,
sorting, city-level locations, age brackets, exclusions — goes through
`--filters-json`, which takes a **file path** or `-` for stdin. Its contents
must be a **JSON object** of `search_params`.

**Merge rule:** top-level keys from the JSON *replace* whatever the flags set
for that key. It is whole-key replacement, not a blend — a JSON file naming
`follower_count` discards `--followers-min` / `--followers-max` entirely. Keys
the JSON does not mention keep their flag-derived values.

```bash
# From a file
th-cli search --filters-json ./filters.json --size 50

# From stdin
cat <<'JSON' | th-cli search --filters-json - --page 0 --size 30
{
  "keywords": ["yoga", "pilates"],
  "follower_count": { "gte": 20000, "lte": 300000 },
  "general_er": { "gte": 2 },
  "languages": ["eng"],
  "is_verified": false,
  "sort": [{ "follower_count": "desc" }]
}
JSON
```

### Full filter reference (30 keys)

Every key is optional; they all **AND** together. A JSON `null` counts as
absent. **Unknown keys are silently dropped**, so check your spelling — and note
that `size`, `page`, `_source` and `track_total_hits` are *not* accepted inside
`search_params` (they would let a caller sidestep the 1–50 bound).

**Shape is enforced.** The API validates the container type of every key below
and answers 422 naming the offending filter. A wrong shape is rejected rather
than dropped, precisely because a dropped filter would run a broader search than
you asked for — and bill you for it.

#### Keyword filters

| Key | Shape | Meaning |
|-----|-------|---------|
| `keywords` | array of strings | Phrases matched (OR) against username / full name / bio. |
| `required_keywords` | **string** | **Comma-separated** phrases, all of which must match: `"vegan,recipes"`. Not an array. |
| `exclude_keywords` | **string** | Comma-separated phrases, none of which may match. Not an array. |

#### Range filters — all `{ "gte": n, "lte": n }`, both bounds optional

| Key | Units |
|-----|-------|
| `follower_count` | count |
| `following_count` | count |
| `media_count` | count |
| `median_likes` | count |
| `median_comments` | count |
| `general_er` | percent (`2.5` = 2.5%) |
| `follower_growth_7` | percent |
| `follower_growth_30` | percent |
| `follower_growth_90` | percent |
| `last_post_at` | **days ago** — `{"gte": 30}` = posted **within** the last 30 days. ⚠️ `lte` is the opposite: `{"lte": 30}` = **no** post for 30+ days (inactive accounts). |
| `audience_authentic` | percent — **premium** |
| `aqs` | Audience Quality Score — **premium** |

⚠️ `audience_male` is **not** accepted. It is the server's internal name for the
normalised `audience_gender` filter; send `audience_gender`.

#### Classification and identity

| Key | Shape | Meaning |
|-----|-------|---------|
| `pks` | array of numbers/numeric strings | Restrict to specific Instagram account ids. |
| `languages` | array of strings | **3-letter** lowercase codes (ISO 639-2, bibliographic form): `spa`, `eng`, `cat`, `glg`, `fre`, `ita`, `por`. French is `fre`, not `fra`. The CLI rejects 2-letter codes here too. |
| `instagram_category` | array of strings | Instagram business categories. |
| `mega_categories` | array of strings | trendHERO's coarse topic buckets. |
| `with_contacts` | array of strings | `biography_contacts`, `trendhero_contacts`. |
| `account_type` | scalar (string or number) | Single `term` match. Never an array or object. |
| `gender` | array of ints | `0` none · `1` male · `2` female · `3` undetermined. **Every element must be one of those four** or the request is a 422. |
| `brand` | array of ints | Same codes, merged into `gender` server-side. The UI sends `[1,2]` for "influencer" and `[0,3]` for "brand". |
| `is_verified` | **boolean** | Real boolean. `"true"` (the string) is a 422. |
| `is_private` | **boolean** | Real boolean. |

#### Locations — the ACCOUNT's location

`locations` is an **array of objects**, each of which **must** carry a boolean
`should_be_included`. The server splits the list into an include half (`true`)
and an exclude half (`false`); an entry with neither is in neither half, so the
API rejects it rather than silently widening the search.

```json
{
  "locations": [
    { "type": "country", "country": "US", "should_be_included": true },
    { "type": "city",    "value": 2643743, "name": "London", "should_be_included": true },
    { "type": "country", "country": "RU", "should_be_included": false }
  ]
}
```

- `type: "country"` reads `country` (ISO code).
- `type: "city"` reads `value` (a GeoNames city id).
- `name` is carried for round-tripping and ignored by the query.

#### Audience locations — **premium**

`audience_locations` is an array of objects describing where an account's
*followers* are, with an optional share range. There is no include/exclude flag
here; every entry is a requirement.

```json
{
  "audience_locations": [
    { "type": "country", "country": "US", "gte": 30 },
    { "type": "city",    "value": 2643743, "gte": 5, "lte": 40 }
  ]
}
```

`gte`/`lte` are the audience share **in percent**. An entry with no bounds means
"has any audience there". An entry naming no place is dropped.

#### Audience gender — **premium**

```json
{ "audience_gender": { "gender": "female", "gte": 60 } }
```

Only `gender` (`male`|`female`) and `gte` are acted on — the index stores the
male share only, so a `female` filter is inverted server-side. **`gte` must be
present and non-blank**; a blank `gte` disables the filter entirely (and, with
it, the premium billing tariff). An `lte` is accepted but ignored.

#### Age brackets

⚠️ `rapidapi_age` is an **ARRAY of range objects**, not a single range — the
brackets OR together. This is the single most common shape mistake:

```json
{ "rapidapi_age": [{ "gte": 18, "lte": 24 }, { "gte": 25, "lte": 34 }] }
```

`{"rapidapi_age": {"gte": 18, "lte": 30}}` — a bare object — is a **422**.

#### Sorting

⚠️ `sort` is an **ARRAY** of single-clause objects, not one object:

```json
{ "sort": [{ "follower_count": "desc" }, { "general_er": "desc" }] }
```

Sortable fields: `general_er`, `follower_count`, `follower_growth_30`,
`median_likes`, `median_comments`. Directions: `asc`, `desc`. Any other key in
a clause is dropped, and `pk asc` is always appended as the final tiebreaker.
`{"sort": {"follower_count": "desc"}}` — a bare object — is a **422**.

### Premium filters change the billing

Four filters are **premium**: `audience_locations`, `audience_authentic`,
`audience_gender` and `aqs`. Using any of them (in a way that actually produces
a query clause) bills the search per hit against the *premium* results balance
instead of the standard one — it does not pay both. If your premium balance is
exhausted the whole search is a 422 and nothing is deducted.

## Paging — do it deliberately

```bash
# Page 0 first, then decide from total_pages whether more pages exist AND are affordable.
th-cli search --keywords travel --size 50 > page0.json
jq '.pagination' page0.json
# { "page": 0, "size": 50, "total_pages": 6, "total_results": 48231 }
```

- Valid pages are `0 .. total_pages - 1`. Page `0` is **always** valid.
- For `--page >= 1`, a page at or beyond `total_pages` is a **422 (exit 6)** and
  is **not billed**.
- `total_pages` is derived partly from your *remaining* `discovery_results`
  allowance, so it can shrink between calls as you spend. Re-read it from each
  response rather than caching it.
- There is no `--all`. If you genuinely need N pages, loop explicitly and
  understand that each iteration is a separate charge.

## Error reference

Failures print `{"error":…,"hint":…}` to **stderr** and exit non-zero. The
server's own message is surfaced verbatim in `error` — read it.

| Exit | HTTP | Cause | What to do |
|------|------|-------|------------|
| 1 | — | Caught locally, **no API call made and nothing charged**: `--size` outside 1–50, negative `--page`, unknown `--gender` / `--with-contacts` value, unreadable or malformed `--filters-json`. | Fix the command. |
| 2 | 401 | Missing or invalid token. | Set `TRENDHERO_TOKEN` — see `auth.md`. |
| 3 | 403 | Either no **AdvancedApi** subscription pack, or an administrator has disabled the `search` capability for this account (a distinct message — read it). | Check the account's plan, or contact support. |
| 6 | 422 | Insufficient balance ("You have reached your search limit…"). | Nothing was returned. The account needs more search credits. |
| 6 | 422 | `size` outside 1–50 — **rejected, never clamped**. Only reachable if the local check is bypassed. | Use 1–50. |
| 6 | 422 | Page beyond the plan cap or beyond the last page. **Unbilled.** | Read `total_pages` from page 0 and stay inside it. |
| 6 | 422 | A filter in the wrong shape — e.g. "The `sort` filter must be a JSON array of objects." **Unbilled.** | The message names the offending filter; fix its container type. |
| 6 | 422 | `search_params` that is not a JSON object at all. **Unbilled.** | Send an object, or omit it entirely for "match everything". |
| 5 | — | Network error or timeout. | Retry; check connectivity / `--base-url`. |
| 7 | 503 | Service temporarily unavailable. | Retry shortly. |

Every 422 above is **unbilled except insufficient balance** (which is by
definition a failure to charge). The 403s and the 401 are unbilled too. Only a
200 costs credits.

## Recipes

```bash
# Usernames + follower counts, one page
th-cli search --keywords "sustainable fashion" --followers-min 25000 --size 30 \
  | jq -r '.results[] | "\(.username)\t\(.follower_count)"'

# Which hits do we already own a report for? (free to fetch)
th-cli search --country US --category Photographer \
  | jq -r '.results[] | select(.paid) | .username'

# Premium: US-audience accounts, sorted by engagement
cat <<'JSON' | th-cli search --filters-json - --size 25
{
  "audience_locations": [{ "type": "country", "country": "US", "gte": 50 }],
  "follower_count": { "gte": 50000 },
  "sort": [{ "general_er": "desc" }]
}
JSON

# Deliberate two-page walk, checking the cap between calls
th-cli search --keywords travel --size 50 --page 0 > p0.json
pages=$(jq -r '.pagination.total_pages' p0.json)
if [ "$pages" -gt 1 ]; then
  th-cli search --keywords travel --size 50 --page 1 > p1.json
fi
```

## Search → report handoff

Search returns usernames; reports are keyed by username. The natural flow is:

```bash
user=$(th-cli search --keywords fitness --country US --size 5 \
        | jq -r '.results[0].username')

# `paid: true` in the search hit means the report is already owned — get it free.
th-cli report get "$user" --wait
```

See `reports.md` for the ordering workflow when no report exists yet.
