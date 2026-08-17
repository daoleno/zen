# Engineering Health Record - 2026-08-17

## Scope and invariants

This branch audits the tree at baseline `53fd3d3`. It does not merge `main` and
does not change Skills or Plugins paths.

The repaired behavior is bounded by five invariants:

1. Daemon-pinned metadata is authoritative for known Codex model identities;
   discovered and installed metadata still passes through for unknown models.
2. Calendar renders, mutates, and synchronizes notifications only for the
   canonical current server. A server switch clears stale modal state.
3. Work, Calendar, and Brain Work subscriptions all end when `RunWithReady`
   returns, including when listener acquisition fails.
4. The daemon CI job requires tests, `go vet`, and build, in that order.
5. The unreferenced Ghostty npm archive remains deleted and verification does
   not leave Python caches or other generated residue.

## Fixed findings

### EH-01: known Codex metadata used a volatile authority

- Problem: `mergeModelPresentationMetadata` preserves its primary argument,
  but the installed Codex cache was primary over the daemon catalog.
- Evidence: a host cache could label `gpt-5.6-sol` differently, set default
  effort to `low`, and replace its effort set although the daemon pins
  `GPT-5.6-Sol`, `medium`, and five supported efforts.
- Fix: known pinned fields are now primary for discovered models and for
  configured required models absent from discovery. Installed metadata still
  fills gaps, and unknown gateway model IDs remain selectable.
- Proof: the regression uses the existing installed-cache fixture with
  deliberately conflicting metadata and asserts the exact pinned projection
  plus unknown-model availability.

### EH-02: Calendar aggregated retained server state

- Problem: the route flattened every `byServer` entry, chose the first
  hydrated server, and synchronized notifications for all retained entries.
- Evidence: after a current-server switch, rows and modal actions could retain
  the previous server even though the connection owner had rebound.
- Fix: one pure selector resolves the exact current-server key. Rows, actions,
  notification sync, add availability, and deep-link lookup derive from that
  server only; selection, editing, and errors clear when the key changes.
- Proof: a two-server reducer fixture asserts the complete current projection
  and asserts null for both a missing key and no current key.

### EH-03: event subscriptions had split teardown ownership

- Problem: the broadcaster unsubscribed Calendar and Brain Work, never Work,
  and did not start when `net.Listen` failed.
- Evidence: all three concrete stores retain subscriber channels until
  `Unsubscribe` or store shutdown, so failed or repeated embedded lifecycles
  could retain a dead Server consumer.
- Fix: `RunWithReady` defers one teardown for Work, Calendar, and Brain Work
  before acquiring the listener. The broadcaster no longer owns a subset.
- Proof: the existing normal-start and listen-failure tests now construct all
  three real stores and require every subscription channel to be closed after
  `RunWithReady` returns.

### EH-04: daemon CI omitted static analysis

- Problem: local `go vet ./...` passed, but ordinary CI did not require it.
- Fix: the daemon job runs tests, vet, then build.
- Proof: the existing workflow contract suite matches all three commands in
  order inside the daemon job, rather than accepting a command elsewhere.

### EH-05: deterministic repository residue

- Problem: `app/ghostty-web-0.4.0.tgz` was an unreferenced 645,565-byte package
  archive, and tracked Python tests created unignored bytecode caches.
- Evidence: repository references and package manifests do not consume the
  archive; native Terminal artifacts are owned under
  `app/modules/zen-terminal-vt`.
- Fix: delete the archive and ignore `__pycache__/` and `*.py[cod]`.
- Proof: reference search is empty outside this record, ignore checks match
  nested cache paths, and the final worktree contains no generated caches.

## Secondary simplification

The first implementation in `d098762` was reviewed against `53fd3d3` and then
reduced without removing a behavior or failure path:

| Category | `d098762` | Final | Net reduction |
| --- | ---: | ---: | ---: |
| Production | `+93/-50` (net +43) | `+71/-49` (net +22) | 21 lines |
| Tests | `+169/-2` (net +167) | `+133/-47` (net +86) | 81 lines |
| Configuration | `+4/-0` (net +4) | `+4/-0` (net +4) | 0 lines |
| Documentation | `+386/-0` | `+155/-0` | 231 lines |

Specific removals:

- Deleted the single-use Calendar items selector and exported enriched-item
  type. The route maps the selected server's already-sorted items directly.
- Reused `installTestCodexModelCache` and `testCodexCacheEntry` instead of
  rebuilding JSON, environment, and file setup in another test.
- Merged the three subscription assertions into the existing successful-start
  and listen-failure tests; both paths and all three channels remain asserted.
- Centralized listener reservation/release and reused a snapshot helper across
  Calendar reducer tests.
- Removed the Server `sync.Once` field and closure. Store unsubscribe methods
  are already idempotent, and a Server runtime is single-use.
- Collapsed repeated CI command-position assertions into one ordered,
  daemon-job-scoped expression.
- Removed the audit inventory, command transcript, repeated tool output,
  generic repository description, and facts recoverable directly from Git.

The remaining additions have concrete owners:

- `fallbackMetadata` is required for configured known models that are not in
  discovery IDs; removing it would restore volatile cache authority there.
- `serverIdOverride` is restricted to the opt-in development screenshot
  runtime, whose live current-server bootstrap is intentionally bypassed.
- The server-scope ref prevents the initial effect from erasing an injected
  error while still clearing stale modal state on a real server switch.
- Real Work, Calendar, and Brain stores are necessary in the teardown tests;
  callback mocks would not prove subscriber maps and channels are released.

## Verification

| Check | Result |
| --- | --- |
| Targeted model authority test | Pass with conflicting installed metadata |
| Targeted Server startup tests | Pass for normal shutdown and listen failure |
| Targeted Calendar and CI tests | 8 pass, 0 fail |
| `cd daemon && go test ./... -count=1` | Pass, all packages |
| `go test -race ./modelprofiles ./server ./work ./calendar ./brain -count=1` | Pass |
| `cd daemon && go vet ./...` | Pass |
| `cd daemon && go build ./...` | Pass |
| `cd app && bun test` | 1,116 pass, 4 explicit skips, 0 fail; 172 files |
| `cd app && bunx tsc --noEmit` | Pass |
| Android and iOS Expo exports | `d098762` evidence remains applicable; route membership, dependencies, and native configuration did not change in the simplification |
| `git diff --check` | Pass |
| Expo Router residual checks | No test/spec files or `bun:test` imports under `app/app` |
| Scope and residue checks | No Skills/Plugins diff, archive, Python cache, export output, or temporary artifact |

## Unresolved risks

- Expo Doctor still reports the Hermes V1 memory-regression versions and 22
  Expo SDK package mismatches. A coordinated SDK 57 patch update needs Android
  and iOS native/device evidence; suppressing the signal is not acceptable.
- Stats and Sessions still contain multi-server workflows. Converging them on
  the current-server owner changes navigation and creation semantics and needs
  a separate product-scoped change with cross-screen switch tests.
- `work.Store` does not own and join its debounce timers and watcher goroutine
  on close. A correct fix needs synchronized stop-and-join behavior and race
  tests; a local timer patch would risk `WaitGroup` and callback races.
- The repository has no lint command or policy. Adding one here would create a
  dependency and formatting migration rather than remove bounded debt.
- Several daemon and App modules remain broad. Size alone does not justify
  speculative splitting across protocol and lifecycle boundaries.

Durable Brain migrations, Pairing V1/V2, unknown-model passthrough, installed
Codex fallback for unknown identities, and WebSocket reconnect/snapshot
fallback remain reachable and tested. They were reviewed but not removed.
