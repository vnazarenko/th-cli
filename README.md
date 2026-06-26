# th-cli — trendHERO for Claude Code

A **Claude Code plugin** that lets your agent pull **trendHERO Instagram
influencer analytics** — ranked top profiles and per-account audience/engagement
reports — just by asking in natural language.

Under the hood it's a small Go CLI (`th-cli`); the skill drives it for you and
reads the JSON back, so you get answers, not raw API output. Standalone CLI
details live in **[`docs/CLI.md`](docs/CLI.md)**.

## Install

```text
/plugin marketplace add https://github.com/vnazarenko/th-cli.git
/plugin install th-cli@vnazarenko-cc-th
```

The skill **self-provisions** its binary on first use (downloads a small,
checksum-verified build from this repo's public GitHub Releases) — **no token is
needed to install**, and it works on macOS and Linux.

## Use it — just ask

Once installed, the skill auto-triggers on Instagram-analytics requests. For
example:

- *"Top Instagram influencers in the US this month"* → ranked profiles
- *"Pull the engagement report for @nasa"* → audience quality, ER, demographics
- *"Order a fresh report for cristiano"* → generates a **new** report (**paid** —
  only on your explicit ask)

The agent decides whether to fetch a report you already have (free) or order a
new one (paid, and only when you clearly ask), reads the status and exit codes,
and summarizes the result.

## Get a token (for reports)

**Top profiles need no token.** Per-account **reports** require a trendHERO
**AccessToken**:

1. Create one at **https://trendhero.io/app/api/access-tokens** (requires the
   **AdvancedApi** subscription; one token per Space).
2. Store it — copy the bundled [`config.example.yaml`](config.example.yaml) and
   paste your token:
   ```bash
   mkdir -p ~/.config/th-cli
   cp config.example.yaml ~/.config/th-cli/config.yaml
   # edit it → replace REPLACE_WITH_YOUR_ACCESS_TOKEN
   chmod 600 ~/.config/th-cli/config.yaml
   ```

The config file is the most reliable choice for the skill (a shell `export` may
not reach the binary, depending on how Claude was launched). More options:
[`skills/th-cli/references/auth.md`](skills/th-cli/references/auth.md).

## What it can do

| Ask for… | The skill runs | Auth |
|----------|----------------|------|
| ranked top accounts by country/month | `th-cli top-profiles` | none |
| a report you already have | `th-cli report get <username>` | token |
| a **new** report (paid) | `th-cli report order <username>` | token |

## The CLI underneath

The skill is a thin wrapper over **`th-cli`**, a small static Go binary over
trendHERO's public API — JSON on stdout, every failure class a distinct exit
code. You can also install and use it directly.

**Full CLI reference — install, commands, flags, exit codes, and development —
is in [`docs/CLI.md`](docs/CLI.md).**

## License

[MIT](LICENSE) © Viktor Nazarenko. A token is still required to *use* the report
commands (see above).
