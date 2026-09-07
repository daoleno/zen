# Usage And Pricing

Stats reads supported agent history from the current server. Switching servers
rebinds the data; Zen does not aggregate unrelated servers into one bill.

Reported charges remain reported charges. When a transcript supplies usage but
not a charge, Zen can estimate from a reference catalog. The screen distinguishes
reported, estimated, mixed, and unknown costs, and preserves exact model/provider
identities. A gateway's actual billing may differ from a public reference tariff.

The model overview shows one name/amount line and one tokens/sessions line.
Amounts containing reference estimates use an approximation mark; missing
amounts remain a dash. Provider names appear only to distinguish identical model
IDs. Tap a model for its full selectable name, exact amount, mixed-cost breakdown,
source, refresh time, token details, and any missing-price reason. Summary totals
remain visible at the top; model diagnostics are not repeated in the list.

Provider model discovery and newly observed usage request an asynchronous catalog
refresh. Requests are bounded, repeated requests coalesce, failed refreshes back
off, and the last successful prices remain available. Changed rates recalculate
existing usage. Source, refresh time, stale state, and refresh failure remain
visible in Stats.

For Codex, matching `last_token_usage` and cumulative deltas retain the input
context of each request (including cached input) before daily aggregation.
Supported catalog context tiers are applied to those requests, not to the sum
of a day's tokens. Explicit typed context thresholds take precedence over the
catalog's legacy `context_over_200k` alias. Duplicate token reports do not add
cost. Missing or mismatched request evidence remains unknown; any known portion
is still shown as a partial estimate. Price refreshes reprice retained usage.

Unknown does not mean zero:

- **Price not in catalog**: no matching reference price exists for the exact model.
- **Price found; request context unavailable**: the catalog has a price, but usage
  lacks the context needed to select a supported tier.
- **Some token rates unavailable**: the catalog does not price every reported token type.

Discovery proving that a model exists does not prove its price. In particular,
Zen does not guess a rate for `gpt-6-astra` or substitute another model's price.
Failed catalog requests retry automatically with backoff. Refresh Stats to read
the latest collected result; it does not bypass that backoff. Missing catalog
entries remain unknown until authoritative pricing becomes available.
