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

Confirmation itself is the read-only models request used by the OpenCode
ecosystem (opgginc/opencode-bar):

```
GET https://opencode.ai/zen/go/v1/models
Authorization: Bearer <key>
Accept: application/json
```

The check succeeds only on a 2xx response whose JSON carries the model list
(`data` or `models` array). Auth failure, HTML, rate limiting, gateway
failures, unparseable bodies, and schema drift all fail closed: no card.
This is deliberately read-only; Zen never sends a model request to the Go
inference API for verification.

## Usage windows

The official Go limits are value-based and published in the OpenCode Go
documentation: **$12 per 5 hours, $30 per week, $60 per month**. Current used
percentages and reset times are only exposed by the authenticated dashboard,
so Zen reads them only when dashboard credentials are configured.

### Dashboard credentials

Zen follows the conventions shared with opencode-bar and opencode-quota.
Credentials are resolved in this order:

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
`authCookie` / `auth_cookie` / `cookie`.

Zen intentionally does not scan browser profiles or export browser cookies.

### Dashboard read

```
GET https://opencode.ai/workspace/<workspaceId>/go
Cookie: auth=<authCookie>
Accept: text/html,application/xhtml+xml
```

The page embeds `rollingUsage`, `weeklyUsage`, and `monthlyUsage` objects
(JSON payloads or SolidJS-serialized expressions) carrying `usagePercent` and
`resetInSec`. The daemon parses the three canonical windows and maps them to
the documented limits ($12/$30/$60). Parsing is fail-closed:

- a non-2xx response, a login/redirect page, a rate-limit page, markup drift,
  or missing fields yields **no** usage windows;
- windows that parse are kept, unparseable ones are dropped, and nothing is
  ever guessed;
- a page with zero parseable windows reports usage as unavailable.

Usage is never cached or replayed: the card shows windows only when the
dashboard parsed successfully in the same refresh. A later dashboard failure
downgrades the card to usage-unavailable instead of keeping stale numbers.

## Card states

- Verified subscription + parsed dashboard windows: plan label, per-window
  used percentage, documented limit, and reset time.
- Verified subscription without dashboard credentials or after a dashboard
  failure: plan label with an explicit "live usage unavailable" note and the
  credential setup hint.
- Anything negative (missing credentials, invalid key, network failure,
  ambiguous response): no card at all.

## Security

- The API key, auth cookie, and workspace ID are never serialized into the
  Stats payload, logged, or cached; only credential *source* labels
  (`environment`, config path) are loggable.
- No browser cookie scanning or exporting.
- Dashboard parsing fails closed on expiry, HTML/login responses, rate
  limiting, and schema drift.
