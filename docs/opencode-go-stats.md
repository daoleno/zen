# OpenCode Go Stats

Zen Stats shows an OpenCode Go card only when the daemon positively confirms
that the current OpenCode account holds an active OpenCode Go subscription.
The card appears per connected server, exactly like the Codex subscription
card; there is no feature-local polling loop — the Stats refresh owner already
covers it.

## Subscription confirmation

The daemon reads the official OpenCode CLI auth file
(`~/.local/share/opencode/auth.json`, or the macOS/Windows equivalent) and
requires an `opencode-go` entry of type `api` with a non-empty key. An
OpenCode Zen entry (`opencode`), a shared `OPENCODE_API_KEY` environment
value, a `opencode-go/*` model in the OpenCode config, or historical usage is
not evidence of a Go subscription.

The Go models endpoint (`GET https://opencode.ai/zen/go/v1/models`) is public
— it returns the same payload for valid and invalid keys — so it is used only
to discover the currently served Go model IDs and is **never** treated as
subscription evidence.

Confirmation is a **non-generating invalid-request auth challenge**:

```
POST https://opencode.ai/zen/go/v1/chat/completions
Authorization: Bearer <key>
Content-Type: application/json

{"model":"<discovered-go-model>","messages":[],"max_tokens":-1,"stream":false}
```

The payload cannot generate a completion: an empty `messages` list and a
negative `max_tokens` are invalid for every model, so the gateway rejects the
request after authenticating the key. Only an **exact 400** whose `error.type`
and `error.code` are both `invalid_request_error` confirms that the key is
accepted by the Go service — proof of authentication with zero token usage.

Fail-closed rules:

- `401`/`403`/`429`/`5xx` and network failures are never accepted.
- A `2xx` response is never parsed or accepted, and never confirms.
- HTML, malformed bodies, and unknown error shapes are never accepted.
- Inconclusive probe responses (for example a 400 with a different error
  type, or a 422) move on to the next discovered model; the check fails
  closed when no discovered model yields the exact confirmation (bounded to
  four probes).
- An empty model list fails closed.

## Usage windows

The official Go limits are value-based and published in the OpenCode Go
documentation: **$12 per 5 hours, $30 per week, $60 per month**. Current used
percentages and reset times come only from the authenticated OpenCode web
subscription server-function, so Zen reads them only when dashboard
credentials are configured.

### Dashboard credentials

Zen follows the conventions shared with opencode-bar, opencode-quota, and
CodexBar. Credentials are resolved in this order:

1. Environment: `OPENCODE_GO_WORKSPACE_ID` and `OPENCODE_GO_AUTH_COOKIE`
2. Config override: `OPENCODE_GO_CONFIG_FILE`
3. Standard config files:
   - `$XDG_CONFIG_HOME/opencode-bar/opencode-go.json`
   - `$XDG_CONFIG_HOME/opencode-quota/opencode-go.json`
   - `~/.config/opencode-bar/opencode-go.json`
   - `~/.config/opencode-quota/opencode-go.json`
   - `~/Library/Application Support/opencode-bar/opencode-go.json` (macOS)
   - `~/Library/Application Support/opencode-quota/opencode-go.json` (macOS)

Config JSON accepts `workspaceId` / `workspaceID` / `workspace_id` and
`authCookie` / `auth_cookie` / `cookie`. The workspace ID may be a raw
`wrk_...` id or a full `https://opencode.ai/workspace/...` URL; it is
normalized before use.

Zen intentionally does not scan browser profiles or export browser cookies.

### Usage read (server-function)

Usage is read through the OpenCode web server-function endpoint
`https://opencode.ai/_server` with the fixed `subscription.get` server id
`7abeebee372f304e050aaaf92be863f4a86490e382f8c79db68fd94040d691b4`
(mirroring CodexBar's `OpenCodeUsageFetcher` and its `docs/opencode.md`):

```
GET https://opencode.ai/_server?id=7abeebee...&args=%5B%22<workspaceId>%22%5D
Cookie: auth=<authCookie>
X-Server-Id: 7abeebee...
X-Server-Instance: server-fn:<unique-id-per-request>
Origin: https://opencode.ai
Referer: https://opencode.ai/workspace/<workspaceId>/billing
Accept: text/javascript, application/json;q=0.9, */*;q=0.8
User-Agent: <browser user agent>
```

The workspace ID travels as a URL-encoded JSON array in the `args` query
parameter. When the GET returns 200 but the body carries no usage windows, a
POST to `/_server` is attempted with the same headers and a JSON body of
`["<workspaceId>"]`. Every request uses a fresh `X-Server-Instance`.

The response is `text/javascript` with serialized objects. Parsing tries
strict JSON first (top-level or under `data`/`result`/`usage`/`billing`/
`payload`), then extracts the window objects from the serialized text. The
rolling 5-hour window is required; the weekly window is included when
present; the monthly window is parsed only when it actually exists. Values
are never guessed.

Fail-closed rules:

- `401`/`403`, `429`, `5xx`, and any non-200 response yield no usage.
- Signed-out text (`login`, `sign in`, `auth/authorize`,
  `not associated with an account`, `actor of type "public"`), explicit
  `null` payloads, and malformed responses yield no usage.
- Page HTML is never treated as success evidence.
- A server-function success (at least the rolling window parsed) confirms
  the subscription itself; otherwise the non-generating invalid-request
  auth challenge below is required for the card.

The legacy `/workspace/<id>/go` HTML-page scrape is explicitly deprecated and
is not used; the server-function contract above is the only dashboard usage
path.

## Card states

- Verified subscription + parsed server-function windows: plan label,
  per-window used percentage, documented limit, and reset time.
- Verified subscription (challenge confirmed) without dashboard credentials
  or after a server-function failure: plan label with an explicit "live
  usage unavailable" note and the credential setup hint.
- Anything negative (missing credentials, invalid key, ambiguous challenge,
  network failure): no card at all.

## Security

- The API key, auth cookie, and workspace ID are never serialized into the
  Stats payload, logged, or cached; only credential *source* labels
  (`environment`, config path) are loggable.
- No browser cookie scanning or exporting.
- Server-function parsing fails closed on expiry, signed-out HTML responses,
  rate limiting, null payloads, and schema drift.
