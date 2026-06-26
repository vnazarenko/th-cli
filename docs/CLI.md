# th-cli — CLI reference

`th-cli` is the small, static Go binary the [trendHERO Claude skill](../README.md)
drives. It wraps trendHERO's **public API** (`/api/public/v1`) the way paying API
clients use it: a long-lived `Authorization: Bearer <AccessToken>`. Every result
is **JSON on stdout** and every failure class is a distinct **exit code**, so
automation can branch without scraping text. You can use it standalone — you
don't need Claude Code.

| Command | Auth | Notes |
|---------|------|-------|
| `th-cli top-profiles` | none | Ranked top Instagram accounts for a country/period |
| `th-cli report get <username>` | token | Fetch an audience/engagement report (add `--wait` to poll) |
| `th-cli report order <username>` | token | Order a **new** report — **PAID**, guarded by `--confirm` |
| `th-cli version` | — | Build metadata as JSON |

## Install

### Prebuilt binary (recommended)

Download the binary for your platform from
[GitHub Releases](https://github.com/vnazarenko/th-cli/releases) and put it on
your `PATH` (verify against `SHA256SUMS`):

```bash
# macOS arm64 example — pick the asset matching your OS/arch
curl -fsSL -o th-cli https://github.com/vnazarenko/th-cli/releases/latest/download/th-cli-darwin-arm64
chmod +x th-cli && sudo mv th-cli /usr/local/bin/th-cli
```

### With Go

```bash
go install github.com/vnazarenko/th-cli@latest   # installs the binary as `th-cli`
```

### From source

Requires **Go 1.24+**.

```bash
make build          # produces ./th-cli in the repo root
./th-cli version
```

Drop the resulting `th-cli` binary anywhere on your `PATH` (e.g. `/usr/local/bin`).

### Building the release matrix locally

`make release` cross-compiles the static binaries for macOS and Linux (amd64 +
arm64) into `dist/` — the same set the `release` workflow publishes to GitHub
Releases on a tag:

```bash
make release
ls dist/            # th-cli-darwin-amd64  th-cli-darwin-arm64  th-cli-linux-amd64  th-cli-linux-arm64
```

## Authentication

Report commands need a trendHERO **AccessToken** — a long-lived token (no
expiry, no `th-cli login`). Get one at
**https://trendhero.io/app/api/access-tokens**; this requires the **AdvancedApi**
subscription, and there is one token per Space.

`top-profiles` is unauthenticated — a token is sent only if one is configured,
never required.

Provide the token via flag, environment variable, or config file
(`--token` > `TRENDHERO_TOKEN` > config-file `token`).

### Config file (recommended)

Copy the bundled [`config.example.yaml`](../config.example.yaml) and paste your
token:

```bash
mkdir -p ~/.config/th-cli
cp config.example.yaml ~/.config/th-cli/config.yaml
# edit ~/.config/th-cli/config.yaml → replace REPLACE_WITH_YOUR_ACCESS_TOKEN
chmod 600 ~/.config/th-cli/config.yaml
```

It is read on every call (no restart needed). The file is just:

```yaml
token: <AccessToken>
```

### Environment variable

```bash
export TRENDHERO_TOKEN=<AccessToken>
```

If you set this in a shell profile, restart your terminal (or Claude Code) so
the value is inherited.

### Base URL (optional)

The CLI uses the trendHERO API at **`https://trendhero.io`**, so you normally set
nothing. It configures only the **host** (it appends the `/api/public` prefix
itself); to point elsewhere, override with `--base-url https://<host>` or
`TRENDHERO_BASE_URL`.

## Usage

```bash
# Ranked top US profiles (no token needed)
th-cli top-profiles --country US

# Filter by ranking type / period
th-cli top-profiles --country UA --type absolute --year 2026 --month 5

# Fetch a report as-is (may still be `collecting`)
th-cli report get nasa

# Fetch and poll until the report reaches a terminal status
th-cli report get nasa --wait --timeout 5m --interval 10s

# Order a NEW report (PAID — spends credits). Refuses without --confirm.
th-cli report order nasa --confirm
th-cli report order nasa --confirm --wait      # order, then poll to completion
```

`top-profiles` flags: `--country` (one of `US UA RU DE FR TR BR IT PL`),
`--type absolute|relative` (default `absolute`; `relative` is not yet
implemented server-side and returns 422 / exit 6), `--year`, `--month` (1-12) —
all optional.

### Reports: status & exit codes

A report's meaningful status is nested at **`report.status`** in the JSON, not a
top-level field. `--wait` stops at any **terminal** status and exits `0`:

- `ready`, `recollecting`, `impossibleButReady` — usable.
- `impossible` — terminal but failed (private/too-small account, etc.).
- `collecting` — still generating; `--wait` keeps polling.

Ordering a report **spends credits**, so it is guarded: `report order` makes no
API call unless you pass `--confirm` (or set `TRENDHERO_ALLOW_WRITES=1`).

Failures print `{"error":...,"hint":...}` to **stderr** and exit non-zero:

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | usage / generic (invalid flag, order without `--confirm`) |
| 2 | auth — missing/invalid token |
| 3 | forbidden (403) — subscription/feature/balance |
| 4 | not found (404) |
| 5 | network / timeout (incl. `--wait` timeout) |
| 6 | validation (422) — e.g. `not_enough_balance` |
| 7 | service unavailable (503) |

## How the skill ships the binary

This repo is **both a Claude plugin and a single-plugin marketplace**
(`.claude-plugin/plugin.json` + `.claude-plugin/marketplace.json`). The skill in
`skills/th-cli/` bundles a tiny launcher (`skills/th-cli/bin/th-cli`) that
**self-provisions** the real binary on first use: it downloads the platform
build from this repo's public GitHub Releases, verifies its checksum, caches it
(under `${CLAUDE_PLUGIN_DATA}` or `~/.cache/trendhero-th/`), and exec's it. No
token is needed to install.

For local development you can symlink the skill into your personal dir and skip
downloading entirely:

```bash
make install-skill                  # links skills/th-cli -> ~/.claude/skills/th-cli (idempotent)
export TRENDHERO_TH_BIN=$PWD/th-cli # point the launcher at a freshly built binary
```

### Cutting a release (publishing the binary)

The launcher downloads the version pinned in `skills/th-cli/VERSION`. Keep that
file, `plugin.json`'s `version`, and the git tag in lockstep:

1. Bump `skills/th-cli/VERSION` (e.g. `v0.3.0`) and `.claude-plugin/plugin.json` `version` (`0.3.0`); commit.
2. Tag and push the tag: `git tag v0.3.0 && git push origin v0.3.0` (tag push only).
3. The `release` GitHub Actions workflow cross-compiles the matrix, writes `SHA256SUMS`, and publishes a **GitHub Release** with those assets — where the launcher fetches them.

Consumers need nothing — GitHub Release downloads are public. The workflow uses
the built-in `GITHUB_TOKEN`, so there are no secrets to configure. Docs/skill-only
changes (no binary change) don't need a release — just push to `main`.

## Development

```bash
make build      # build ./th-cli with version metadata injected via -ldflags
make test       # go test ./...
make lint       # go vet (+ golangci-lint if installed)
make generate   # regenerate internal/api/client.gen.go from the OpenAPI spec
make release    # cross-compile dist/ binaries (mac/linux × amd64/arm64)
make clean      # remove build/test artifacts
```

### OpenAPI & code generation

The typed API client (`internal/api/client.gen.go`) is **generated** from the
authored spec `internal/api/public-api.openapi.yaml` by
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) (pinned in
`tools/tools.go`). The spec is the machine-readable contract for the public API
and the single source of truth for the client.

After editing the spec, regenerate and commit the result:

```bash
make generate           # writes internal/api/client.gen.go
git add internal/api/client.gen.go
```

CI verifies the checked-in client matches the spec (a generate that produces a
diff fails the build).

### Layout

```
main.go                  entrypoint → cmd.Execute()
cmd/                     Cobra commands (root, version, top-profiles, report get/order)
internal/api/            OpenAPI spec, generated client, client wrapper + poll-until-ready
internal/config/         flag > env > file resolution, host + token resolution
internal/output/         JSON writer + error→exit-code mapping
internal/skill/          SKILL.md frontmatter + launcher-bundle validation (doc-lint)
skills/th-cli/           the th-cli Claude skill (SKILL.md, references, VERSION, bin/th-cli launcher)
.claude-plugin/          plugin.json + marketplace.json (this repo is a plugin AND its marketplace)
config.example.yaml      token config template
tools/tools.go           build-time oapi-codegen tool pin
```
