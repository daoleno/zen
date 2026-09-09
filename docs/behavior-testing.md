# Executable Behavior Contracts

Zen uses ordinary Go tests with stable `TestBDD_ZENnnn_...` names, comments
describing Given/When/Then, and assertions against production state and effects.
There is no Cucumber dependency, custom scenario language, scheduler or test
agent. A passing provider exit or a model's assertion of PASS is not an oracle.

## Scenarios

| ID / executable suffix | Given | When | Then |
| --- | --- | --- | --- |
| ZEN001 `DurableDecisionCleanup/done` | Admitted delegated Worker | Exact terminal report emits Work change, then scripted Brain accepts | Durable result reaches Brain before Work closes; only completed owned Session is removed |
| ZEN001 `.../failed` | Worker failure | Scripted Brain inspects failure and cancels | Failure remains failure, not implicit success |
| ZEN001 `.../accept-before-cleanup-crash` | Decision saved without teardown | Store reopens and cleanup reconciles | Saved decision survives and exact Session is reclaimed idempotently |
| ZEN001 `.../cleanup-failure` | Injected teardown failure | Decision persists; explicit recovery reconciles after fault removal | Failure is visible, acceptance durable, no business input replay |
| ZEN001 `.../reused-session` | Closed old Work, new turn in same Session | Old cleanup runs after reopen | New owner, user Session and Host survive |
| ZEN002 `StaleDeliveredHandlerIncident` | Historical delivered handler never ended | Independent Worker completes, including transcript replacement/restart | New result reaches free Brain lane without manually deleting old handling; summary is terminal evidence |
| ZEN003 `BusyBrainDefersUntilProviderTurnEnds` | Exact foreground provider Activity running | Result arrives, then foreground terminal edge | No interrupt or premature admission; result delivered when lane becomes free |
| ZEN004 `CLIReportToDecisionAndSessionRemoval` | Canonical admission and isolated Unix control server | Actual CLI reports exact turn; scripted Brain explicitly accepts | Production control/Store/Service delivery and cleanup, duplicate report no-op, closure survives reopen |
| ZEN005 `PartialResultNeedsScopedFollowup` | Objective needs A and B; result contains A only | Scripted Brain asks only for missing B, then runtime reopens | Incomplete result is not accepted; later result requires explicit acceptance; no replay of original input |
| ZEN006 `UnrelatedCleanupCannotInvalidateDecision` | Historical cleanup ownership conflict | Independent Work is accepted | Decision succeeds with scoped cleanup; global recovery still reports the genuine historical conflict |
| ZEN007 `CompletedCleanupSerializesWithNewInput` | Old cleanup waiting for input lock | New turn acquires ownership first | Production watcher rejects old cleanup before transport IO |
| ZEN008 `AlreadyReclaimedSessionCleanupIsIdempotent` | Completed ledger identity, missing tmux window | Production watcher cleanup runs twice | Proven absence succeeds idempotently through the actual cleanup boundary |
| ZEN009 `LostOriginalResultSurvivesWaitAndRestart` | Original admitted turn provisionally lost | Brain waits and Store restarts before exact supplied terminal progress | Original result supersedes loss and requires fresh judgment; duplicates dedupe; cancelled/superseded turns cannot revive |
| ZEN010 `IncompleteInventoryDoesNotDeclareOriginalLost` | Discovery snapshot lacks original Session | Authoritative probe says present, unreadable, or absent | Only proven absence records loss; present/unreadable preserves original progress eligibility without replay |
| ZEN011 `RealProviderDecision` | Fresh arithmetic objective and independent numeric oracle | Two real API calls produce Worker result and Brain judgment | Actual delivered result checked, explicit decision persisted, exact cleanup asserted; opt-in hybrid, not native E2E |
| ZEN012 `ProviderPathBudgetAndFailures` | Same provider harness using scripted HTTP | Full loop, 429, truncation, invalid envelope or timeout | Correct side effects or classified failure; no retry and no third call |
| ZEN013 `DecisionSavedWithCleanupPending` | Cleanup failure on current completed Session | Control accepts Work | `brain_work_cleanup_pending` includes persisted Work; recovery completes only cleanup, not the business action |
| ZEN014 `QuietMissingSessionCleanupIsAbsentNotUnowned` | Completed ledger identity, tmux 3.6a quiet show-options success for a gone session | Production watcher cleanup runs twice | Proven absence succeeds; not classified as `ErrUnownedTmuxTarget` |
| ZEN015 `UnownedPresentCompletedCleanupIsProtected` | Completed ledger identity, live tmux target without Zen ownership marker | Cleanup reconciled twice | Target is untouched and `ErrUnownedTmuxTarget` stays visible |
| ZEN016 `RealTmuxCompletedCleanupDistinguishesAbsenceAndUnowned` | Isolated tmux socket with reclaimed owned window and later unowned identity reuse | Official completed cleanup | Absence is idempotent; reused unowned window is not killed |
| ZEN017 `RealTmuxWrongSocketAndRebootOwnership` | Completed target on a different socket; leftover owned window after a fresh watcher | Official completed cleanup | Wrong-socket ambient is untouched; reboot leftover owned window is reclaimed; retry is idempotent |
| ZEN018 `StartupReconciliationAbsentCompletedIsIdempotent` | Mixed absent completed Session and genuine unowned present target | `ReconcileSignalSystemStartup` twice | Absent target is reclaimed; unowned/active/Host survive; unowned error remains visible |

ZEN001 also submits duplicate control and bound provider terminals, asserts no
extra input, and reopens the Store. ZEN002 covers interrupted Host handling.
The watcher cleanup lock and missing-resource tests run in the full Go gate;
they exercise the actual watcher boundary rather than the scripted Session map.
Stable IDs are retained when contracts change.

## Local And CI

From `daemon/`:

```sh
go test -count=1 -timeout 120s ./brain ./cmd/zen ./watcher -run '^TestBDD_'
go test -json -count=1 -timeout 120s ./brain ./cmd/zen ./watcher -run '^TestBDD_'
go test ./...
go test -race -p 1 ./...
go vet ./...
go build ./cmd/zen
```

Use `GOMAXPROCS=2`, `-p 1`, and `GOTMPDIR="$ZEN_BUILD_TMPDIR"` in shared,
resource-constrained sessions. Tests use temporary directories and isolated
sockets/HTTP listeners with cleanup, not the user's daemon. Do not restart or
deploy the dev daemon for these gates. Watchers already owned by the user may
react to source changes; that is not a controlled deployment or live proof.

PR CI runs the deterministic contracts and uploads standard `go test -json`
JSONL, including `Test`, `Action`, `Package` and elapsed-time fields. Consumers
must use terminal `pass`/`fail`/`skip` test actions and the process exit status,
not text containing PASS. A skipped ZEN011 is not real-provider success. Keep
failed runs; never rerun unchanged real calls until green. Deterministic test
decisions are scripted evidence, not demonstrations of actual AI judgment.

## Explicit Real-Provider Gate

Reuse the authorized active connection and inspect its compiled protocol/auth
binding, not just a provider catalog label. Bind `ZEN_BDD_PROTOCOL`
(`responses` or `chat_completions`), `ZEN_BDD_BASE_URL`, `ZEN_BDD_MODEL` and
`ZEN_BDD_API_KEY` from that same connection; do not mix a credential with an
unrelated endpoint or require a new provider account. The test never changes
Zen's current provider or reads/modifies live lifecycle state.

```sh
ZEN_BDD_REAL_PROVIDER=1 ZEN_BDD_MAX_CALLS=2 \
  go test -json -count=1 -timeout 120s ./brain \
  -run '^TestBDD_ZEN011_RealProviderDecision$'
```

The four bound configuration values must already be in the environment;
never put credentials in command arguments, artifacts or the repository.
Reuse Zen's provider catalog and private credential-store reference when
preparing the child environment, without printing secrets. Check provider
metadata before spending the two-call budget. A catalog's `openai` label alone
does not prove Chat Completions permission or the connection's auth mode.
The selected connection must support Bearer authentication for the chosen API.
There is no automatic protocol/model/provider fallback after a denial.

Chat Completions uses `model`, `messages`, `max_tokens` and `stream:false`.
Responses uses `model`, `input`, `max_output_tokens:128`, `stream:false`,
`store:false`, and `reasoning:{effort:"low"}`. Select a Responses model whose
documented/configured capabilities support that effort. The token cap includes
reasoning tokens; insufficient budget is a retained incomplete response, not
permission to increase the cap or retry. Only completed assistant `output_text`
is read; reasoning content is ignored, refusal/incomplete output cannot pass.
JSON is prompted and independently parsed/checked without special JSON mode.
These shapes follow the official [Responses create reference](https://developers.openai.com/api/reference/resources/responses/methods/create)
and [authentication/request-ID guidance](https://developers.openai.com/api/reference/overview).
Zen's existing endpoint builder and safe HTTP transport enforce URL validation,
no ambient proxy and no redirects; the real gate requires HTTPS.
Bounds: at most **two HTTP requests**, **2048 input
bytes per request**, **128 maximum output tokens per request**, **40 seconds
per HTTP request**, **90 seconds for the shared provider context**, and a
**120-second Go test deadline**. No tools, background agents or native provider
processes are launched. This is a token/request budget, not a guaranteed dollar
price; the operator must approve the selected model's current pricing.

`behavior-provider.yml` provides the same manually dispatched gate with an
explicit budget checkbox. Its repository variables `ZEN_BDD_BASE_URL` and
`ZEN_BDD_MODEL` and `ZEN_BDD_PROTOCOL` must be bound to repository secret `ZEN_BDD_API_KEY`; there is
no dispatch-time endpoint override that could redirect the secret elsewhere.
It is never a PR dependency. No configured secret,
model or budget acknowledgment is an `environment_failure`, not a skip.
Transport, HTTP errors (including authentication/rate limits), truncation and
invalid protocol envelopes are `provider_environment_failure`. Valid provider
content that fails the independent oracle or explicit decision contract is
`behavior_failure`. None is converted to success or retried. No response bodies
or keys are logged. Failed HTTP calls retain status, call index, recognized
allowlisted error code/type and a bounded validated `x-request-id` when present;
arbitrary error messages, unknown enum values and headers are not logged.
This improves future failure evidence without reconstructing missing details
from an older run or treating every 403 as a proven permission diagnosis.
Successful structured evidence is emitted only after state,
delivery, persisted decision, cleanup and unrelated-Session assertions pass.

The real-provider path deliberately isolates reasoning from native transport:
Session sends/removal use the existing fake watcher, but Store, lifecycle,
event admission and explicit disposition use production code. The scripted
ZEN012 path verifies that harness offline. Neither proves native provider
launch/discovery or actual automatic Brain execution. A native no-watch
experiment requires its own producer-triggered Brain event, actual model
decision and observed owned Session removal; keep its private identities and
evidence in Brain `worklog/`, not this repository. Existing real acceptance
evidence need not be rerun just because deterministic coverage expands.

## Brain Card Presentation

`TestBDD_BrainCardUnicodeDeliveryHistoryAndAcceptance` runs the actual progress
validator, direct-event emitter and parser, persisted provider/timeline projection,
duplicate delivery and explicit review acceptance. When Bun is installed, its
API payloads execute the mobile timeline projection for live, accepted and
reconnected history. This is deterministic transport testing, not a live AI run.
`TestBrainCardPresentationSubscriptionAndReconnect` also exercises the production
WebSocket subscription handler with a legacy escaped-replacement envelope.

Progress summaries retain their byte budget without splitting UTF-8 code points.
Admission remains byte-exact. Presentation recognizes only a complete reserved
event object with the exact schema, nonempty identities and no duplicate keys;
JSON whitespace, key order and Unicode escaping may normalize. Quoted examples,
partial/unknown envelopes and assistant results remain conversation content.
Legacy damaged summary tails are abbreviated only in the card projection; the
stored evidence is not rewritten. Diagnostic JSON is available in card details,
not expanded into default-card counters. Work lifecycle and diagnostic changes
invalidate mobile projection caches even when the event identity is unchanged.

Run `go test ./brain -run BDD_BrainCard` and
`go test ./server -run BrainCardPresentation` from `daemon/`.

## Recovery Limits

Inventory absence is not process death: reconciliation now requires an
authoritative absent Session probe. Unknown transport fails closed. A lost
producer's durable loss fact and last execution fence permit exact late
terminal evidence even after a wait decision clears the provisional review.
Newer execution or terminal Work invalidates that authority. No progress report
is forged to settle an original incident, and passing a reproduction does not
retroactively prove that a previously rejected live report was delivered.

These regressions cover an owned Session missing from discovery and the
wait/restart supplied-progress rejection. They do not prove arbitrary orphaned
native-process recovery after the underlying tmux Session truly disappears.
The process may still run outside discoverable ownership; that uncertainty
must remain distinct from success and must never authorize business replay.
