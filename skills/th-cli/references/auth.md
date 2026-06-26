# Authentication & base URL

`th-cli` authenticates to the report endpoints with a **long-lived trendHERO
AccessToken** sent as `Authorization: Bearer <token>`. There is no `th-cli login`
and no token refresh — the token never expires, mirroring how real API clients
connect. `top-profiles` is unauthenticated and needs no token at all.

## Minting an AccessToken

1. The account must be on the **AdvancedApi** subscription (the API tier).
   Without it, no token can be created.
2. Go to **https://trendhero.io/app/api/access-tokens** and create a token (one
   per Space). Copy the generated private token — it is shown so it can be
   stored.

If a report command exits **2** with a "no AccessToken configured" message, the
token is missing or wrong: re-check the value and that it belongs to an
AdvancedApi account.

## Supplying the token

Highest precedence first (token resolution is `flag > env > file`):

1. `--token <value>` on the command.
2. `TRENDHERO_TOKEN` environment variable — the usual choice:
   ```bash
   export TRENDHERO_TOKEN=<AccessToken>
   ```
3. `token:` key in the config file `~/.config/th-cli/config.yaml`.

## Base URL (host)

`th-cli` uses the trendHERO API at **`https://trendhero.io`**, so you normally
set nothing here. It configures only the **host** (scheme + authority) and
appends the `/api/public` path itself — never include a path in the host. To
point it elsewhere, override the host (highest precedence first): `--base-url`
> `TRENDHERO_BASE_URL` > config `base_url`.

## Config file (optional)

`~/.config/th-cli/config.yaml`:

```yaml
token: <AccessToken>
# base_url: https://trendhero.io   # optional host override
```

The file is optional; env vars and flags work without it.

## Write opt-in

`report order` spends credits and is guarded. Set `TRENDHERO_ALLOW_WRITES=1`
(truthy values: `1`/`true`/`yes`) to allow paid orders for the whole session
instead of passing `--confirm` each time. Leave it unset unless the user has
explicitly authorized spending credits.
