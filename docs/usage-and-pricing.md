# Usage And Pricing

Stats reads supported agent history from the current server. Switching servers
rebinds the data; Zen does not aggregate unrelated servers into one bill.

Reported charges remain reported charges. When a transcript supplies usage but
not a charge, Zen can estimate from a reference catalog. The screen distinguishes
reported, estimated, mixed, and unknown costs, and preserves exact model/provider
identities. A gateway's actual billing may differ from a public reference tariff.

Provider model discovery and newly observed usage request an asynchronous catalog
refresh. Requests are bounded, repeated requests coalesce, failed refreshes back
off, and the last successful prices remain available. Changed rates recalculate
existing usage. Source, refresh time, stale state, and refresh failure remain
visible in Stats.

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
