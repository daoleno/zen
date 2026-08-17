# Zen Engineering Health Audit - 2026-08-17

## Scope and invariants

This audit was performed in the isolated worktree
`engineering-health-2026-08-17`, on branch
`chore/engineering-health-2026-08-17`, from baseline `53fd3d3`.

The audit uses these invariants:

1. One configured server is canonical at a time. Feature data and actions must
   be projected only from that current server.
2. A long-lived subscription, goroutine, timer, process, socket, temporary
   file, or cache must have a named owner and a bounded teardown path.
3. Daemon-pinned model metadata is authoritative for known Codex identities;
   volatile installed-CLI data may add discovered identities but must not
   silently change the daemon contract.
4. Compatibility code is retained only where durable data, a supported wire
   version, or a reachable caller proves that it is still required.
5. Skills/Plugins management files and semantics are excluded. Findings in
   that area are record-only, and no excluded path may change in this branch.

Explicitly excluded paths include `app/app/skills.tsx`,
`app/components/skills/**`, `app/services/skillsManagement*`,
`app/services/skillsScreen*`, `app/services/pluginsSkills*`,
`daemon/skills/**`, `daemon/server/skills_*`, and
`artifacts/skills-manager-*`.

## Repository and ownership map

| Boundary | Entry points | Production owner | Important lifetime / data contract |
| --- | --- | --- | --- |
| Daemon process | `daemon/cmd/zen/main.go`, `daemon/cmd/zen-dev/main.go` | `runDaemon` plus `runJoinedRuntime` | Starts Watcher, Stats, Calendar, control socket, optional Link, and HTTP server under one cancellation context. |
| Mobile HTTP/WebSocket API | `daemon/server/server.go` | `server.Server` | Authenticated clients, Terminal attachments, Work/Calendar/Brain subscriptions, and per-client conversation subscriptions must all end with the server runtime. |
| Agent/session observation | `daemon/watcher` | `watcher.Watcher` | Poll loop and provider evidence are canonical for daemon-visible Session state. |
| Terminal | `daemon/terminal` | `terminal.Manager` and backend `Session` | A client detach transfers physical close work to `terminalCleanupOwner`; `RunWithReady` joins it before return. |
| Provider/model routing | `daemon/modelprofiles` | `modelprofiles.Owner` | Owns profiles, credentials, route bindings, loopback router/gateway, Codex catalog, and live control. Exact model identity passes through; known metadata is pinned. |
| Work files | `daemon/work` | `work.Store` | Owns Markdown index, fsnotify watcher, subscribers, and 200 ms per-path debounce timers. |
| Brain Work/Event | `daemon/brain` | `brain.Store` and `brain.Service` | Append-only facts and current Work review state are daemon-owned; the app receives projections. |
| Calendar | `daemon/calendar` | `calendar.Store` and `calendar.Scheduler` | Store owns items/runs and subscriptions; Scheduler owns its ticker under context cancellation. |
| Optional remote transport | `daemon/link`, `daemon/relay`, `daemon/linkproto` | Link connector / relay server | Preserves the same daemon handler; it is a transport candidate, not another application server. |
| Mobile connection | `app/store/currentServer.tsx`, `app/app/_layout.tsx`, `app/services/websocket.ts` | `CurrentServerProvider` plus `CurrentServerConnectionBinder` | Exactly one stored server is current; binder disconnects old sockets and connects only the current server. |
| Mobile projections | `app/store/{agents,brain,work,calendar}.tsx` | Store reducers keyed by server ID | Keying supports rebinding, but screens must select only the canonical current-server key and clear/rebind on switch. |
| Mobile UI | `app/app`, `app/components` | Expo Router screens and shared components | Android and iOS share TypeScript owners. `app/app` is runtime route code and contains no tests. |
| Native Terminal | `app/modules/zen-terminal-vt` | shared native module | Android/iOS artifacts are generated and verified from a pinned native lock. |
| CI/release | `.github/workflows`, `scripts` | GitHub Actions and checked scripts | CI runs daemon/App tests, builds, exports both mobile bundles, and native contract jobs. |

Repository size is approximately 303,751 lines across the audited Go and App
source sets (Skills exclusions applied): 174 Go production files, 222 Go test
files, 415 App production files, and 168 App test files. Large files were used
only as navigation clues; none was selected for refactoring on size alone.

## Method and baseline evidence

The audit used reference searches (`rg`), tracked/ignored file inspection,
file-size and line-count scans, `git log`, `git blame`, and direct inspection of
constructors, subscriptions, cancellation, timers, temp files, process starts,
compatibility branches, and current-server projections.

Baseline results before production edits:

| Check | Baseline result |
| --- | --- |
| `git status --short --branch` | Clean; `HEAD` is `53fd3d3`; branch tracks `origin/main`. |
| Targeted modelprofiles regression | Fails: known `gpt-5.6-sol` is projected with installed-cache default `low`, while the pinned daemon catalog says `medium`. |
| `cd daemon && go test ./... -count=1` | Fails only `TestAccountCodexProjectionAddsKnownEffectMetadataWithoutGatingUnknownModels`; all other packages pass. |
| `cd daemon && go vet ./...` | Passes. |
| `cd daemon && go build ./...` | Passes. |
| `cd app && bun test` after `bun install --frozen-lockfile` | 1,114 pass, 4 explicit isolation/performance skips, 0 fail across 172 files. The pre-install run had 21 module-resolution errors; those were an environment precondition, not repository failures. |
| `cd app && bunx tsc --noEmit` | Passes. |
| `cd app && bunx expo-doctor` | Fails 2 of 21 checks: Hermes V1 memory-regression versions and 22 Expo SDK package-version mismatches. |
| `python3 -m unittest discover -s scripts/tests -p 'test_*.py'` | 23 pass. It creates untracked `__pycache__` directories because the repository does not ignore Python bytecode. |
| `git diff --check` | Passes. |

CI currently runs Go tests/build, App tests/typecheck, Android+iOS exports, and
native/release contracts. It does not run `go vet`, even though the current
tree passes it. No repository lint command or lint configuration was found;
that is documented rather than inventing a new lint stack.

## Prioritized findings

### P0

No P0 data-loss, secret-exposure, or remotely exploitable issue was proven in
the audited scope.

### P1 - fix in this branch

#### EH-01: known Codex metadata authority is reversed

- Evidence: `model_catalog.go` pins `gpt-5.6-sol` default effort to `medium`.
  `owner_provider.go` says the pinned catalog is the final metadata authority,
  but calls `mergeModelPresentationMetadata(discoveredOrInstalled, pinned)`;
  the merge keeps every non-empty primary field. The current installed Codex
  cache says `low`, and both the targeted test and the full suite project
  `low`. `git blame` shows the assertion and pinned catalog were introduced
  together; the test is not an obsolete expectation.
- Impact: Settings and Session effort defaults vary by host cache, so a
  volatile CLI refresh can silently change a daemon-owned contract.
- Risk: low. Unknown models must remain available and retain cache metadata;
  only exact daemon-known identities should be overlaid by the pinned owner.
- Recommended action: make pinned known-model presentation metadata primary,
  preserve discovered availability/identity, and make the regression fixture
  hermetic with an intentionally conflicting `CODEX_HOME` cache.
- Treatment: **fix now**.

#### EH-02: Calendar bypasses the canonical current-server owner

- Evidence: `app/app/calendar.tsx` builds `items` with
  `Object.values(state.byServer).flatMap(...)`, chooses the first hydrated
  entry as `activeServer`, and syncs notifications for every retained store
  entry. It does not consume `useCurrentServer`. Selected/editor modal state
  can therefore keep an old `serverId` across a switch. This contradicts the
  repository's explicit single-current-server contract.
- Impact: stale or cross-server Calendar rows can be rendered or acted on
  during rebinding, and notification synchronization can run against a server
  that is no longer current.
- Risk: low to medium. Calendar actions are already server-keyed; the change
  constrains projection and actions to the canonical key without changing the
  wire contract.
- Recommended action: add a pure current-server selector to the Calendar
  store, use it for rows/actions/notifications, and clear modal state on a
  current-server change. Test that non-current entries are unobservable.
- Treatment: **fix now**.

#### EH-03: Server event subscriptions do not share one teardown owner

- Evidence: `server.New` subscribes to Work and Brain; `SetCalendar` subscribes
  to Calendar. `broadcastEvents` defers Calendar and Brain unsubscribe but
  never Work unsubscribe. If `net.Listen` fails, `broadcastEvents` never
  starts, so none of those defers can run. Stores retain buffered channels in
  subscriber maps until store shutdown.
- Impact: repeated embedded/server lifecycles retain subscriptions and
  continue fan-out to consumers whose server runtime ended. The production
  daemon normally closes the stores later, but the server's own resource
  ownership is incomplete and failed startup is a proven leak path.
- Risk: low. The concrete stores already expose idempotent `Unsubscribe`
  methods and server runtime is single-use.
- Recommended action: add one idempotent server subscription teardown method,
  defer it from `RunWithReady` before listener acquisition, remove split
  teardown from the broadcaster, and test both normal cancellation and listen
  failure.
- Treatment: **fix now**.

### P1 - record, do not change in this branch

#### EH-04: Expo Doctor is a persistent red signal with a known memory risk

- Evidence: Expo Doctor reports `expo@57.0.4` / React Native `0.86.0` using a
  Hermes V1 revision affected by a documented memory regression; it also
  reports 22 patch/minor SDK alignment differences and requires Expo
  `>=57.0.9` / React Native `>=0.86.2` for the named fix.
- Impact: possible mobile memory growth and a health command that cannot be
  used as a green gate.
- Risk: medium to high for this audit. Correct remediation changes the shared
  lock and many native-facing packages and requires Android+iOS native build
  and device regression evidence.
- Recommended action: perform one dedicated Expo SDK 57 patch-train update,
  regenerate native projects, run both native CI jobs, and collect memory
  evidence. Do not suppress Doctor or add exclusions.
- Treatment: **record only** because dependency coordination/native validation
  exceeds this branch's bounded debt-removal scope.

#### EH-05: additional feature screens retain multi-server projections

- Evidence: Stats derives all connected server IDs and merges `getStats`
  payloads; the Sessions list reads all configured servers, filters/labels
  across them, and constructs multiple create-server options. Calendar is the
  narrowest proven stale-action path and is fixed here, but the remaining
  paths are broader user workflows.
- Impact: these paths encode semantics contrary to the current-server product
  contract, even though `CurrentServerConnectionBinder` normally leaves only
  one live socket and mitigates the common runtime case.
- Risk: medium to high. Removing the remaining behavior affects navigation,
  creation, selection, persisted route parameters, and visible aggregation.
- Recommended action: a dedicated current-server convergence change should
  replace feature-local server enumeration with shared selectors and add
  switch/rebind tests across Sessions, Stats, Terminal routes, Brain, and Work.
- Treatment: **record only**; do not fold a product-wide behavior change into
  the narrow Calendar fix.

#### EH-06: Work watcher does not join debounce timers on close

- Evidence: `work.Store.StartWatcher` owns a local unbounded-by-path map of
  `time.AfterFunc` timers. `Store.Close` closes fsnotify and subscriptions but
  cannot see, stop, or join those timers or the watcher goroutine. A callback
  can stat/reload/broadcast after `Close` returns for up to the debounce window.
- Impact: bounded post-close work in the normal daemon, but repeated embedded
  lifecycles or a high-cardinality event burst can retain Store/path closures
  and violate teardown semantics.
- Risk: medium. A correct repair needs a synchronized debounce owner and a
  goroutine join; naive timer stopping can race `WaitGroup.Add` with `Wait` or
  double-complete replaced timers.
- Recommended action: introduce a tested debounce runtime with stop-and-join
  semantics in a focused follow-up.
- Treatment: **record only** because the server subscription leak has a
  smaller, fully bounded fix and this timer change deserves dedicated race
  design.

### P2 - fix in this branch

#### EH-07: CI omits a passing static correctness gate

- Evidence: `.github/workflows/ci.yml` runs `go test` and `go build`, but not
  `go vet`; local full-module vet passes. Existing `app/ciWorkflow.test.js`
  already treats CI workflow shape as a tested contract.
- Impact: printf, copylock, atomic, and related analyzer regressions can merge
  without a required signal.
- Risk: low.
- Recommended action: add `go vet ./...` to the daemon CI job and strengthen
  the workflow contract test.
- Treatment: **fix now**.

#### EH-08: deterministic tracked/untracked build residue

- Evidence: `app/ghostty-web-0.4.0.tgz` is a 645,565-byte npm package archive
  containing built JS/WASM. Reference search finds no caller or package
  dependency; history shows it entered in the April `chore: checkpoint current
  local work` commit and was never subsequently referenced. Running the
  tracked Python script tests creates untracked `scripts/**/__pycache__`, and
  `.gitignore` has no Python bytecode rule.
- Impact: dead binary weight remains in every clone and routine verification
  dirties the worktree.
- Risk: low; the native Terminal uses the pinned source/artifact contract under
  `app/modules/zen-terminal-vt`, not this archive.
- Recommended action: delete the unreferenced archive and ignore Python cache
  files/directories. Verify reference absence, ignore behavior, and all App
  tests/builds.
- Treatment: **fix now**.

### P2 - record, do not change in this branch

#### EH-09: CI has no project lint owner

- Evidence: neither root nor App package scripts define lint, and no ESLint
  configuration is present. TypeScript and Bun tests are green, but a requested
  `lint` command does not exist.
- Impact: style and some React-specific static checks rely on review and
  TypeScript rather than a stable automated owner.
- Risk: medium. Introducing a lint stack would add dependencies and likely
  create broad policy/format churn.
- Recommended action: establish lint rules in a dedicated change with an
  audited initial baseline; do not autoformat the repository as part of it.
- Treatment: **record only**.

#### EH-10: very broad modules remain review hotspots

- Evidence: `daemon/watcher/watcher.go` (~168 KB),
  `daemon/brain/orchestration.go` (~160 KB), `daemon/server/server.go` (~111
  KB), `app/services/websocket.ts` (~116 KB), and several route/components are
  materially broad. The audit found strong behavior tests and explicit owners
  around their highest-risk lifecycles.
- Impact: review and change-locality cost, but size alone is not a behavioral
  defect.
- Risk: high for speculative splitting because protocol and lifecycle ordering
  are tightly coupled.
- Recommended action: split only when a concrete behavior change exposes an
  owner boundary and can carry contract tests.
- Treatment: **record only**.

## Proven non-issues / retained compatibility

- **Brain schema and timeline compatibility:** the v11-to-v12 orchestration
  migration and legacy timeline-kind normalization are reachable from durable
  on-disk data and are covered by migration/restart tests. They are not dead
  aliases. Older unsupported schemas fail closed with
  `ErrSchedulerStateReset`.
- **Pairing V1 and V2:** V1 remains the documented LAN/tunnel payload and V2 is
  the Link payload. Both have live call sites and protocol tests.
- **Codex unknown-model passthrough:** opaque identities are intentional and
  tested; the fix to known metadata must not turn the pinned catalog into an
  allowlist.
- **Installed Codex cache fallback:** it remains useful for discovered model
  identity and unknown-model presentation. Only known pinned fields require
  deterministic authority.
- **WebSocket reconnect/fallback behavior:** App reconnect timers are canceled
  on intentional disconnect; request listeners remove themselves on reply,
  send failure, or bounded timeout. Conversation snapshot fallback is required
  for revision gaps and reconnect.
- **Terminal cleanup panics:** `terminalCleanupOwner` and
  `terminal.Manager.SetCleanupSubmitter` panic only on internal ownership
  invariant violations. They are not request-controlled fatal paths and have
  lifecycle tests.
- **CLI `log.Fatal` / `os.Exit`:** occurrences are process entry-point error
  handling. Library packages return errors.
- **Temporary files:** audited atomic-write and upload/update paths close and
  remove partial files on error; successful atomic renames transfer ownership.
  App PDF/upload staging has explicit cancellation/deletion behavior tests.
- **Screenshot/performance fixtures:** production-reachable route modules are
  guarded by `__DEV__`, an explicit environment opt-in, and `demo=1`; tests
  prove the live connection runtime is bypassed. This is intentional tooling,
  not hidden production data.
- **Pricing vs capability metadata:** model pricing in `daemon/stats` and model
  capability/effort metadata in `daemon/modelprofiles` are separate domains;
  the duplicate model keys do not represent competing owners of the same
  fields.
- **Skills/Plugins:** findings in excluded files were not acted on. No change
  in this branch may be used to infer a Skills Manager semantic decision.

## Implemented change set

The selected five changes were implemented without changing a wire protocol,
provider inventory, supported user feature, or excluded Skills/Plugins path:

1. **Known Codex metadata authority:** `projectConnectionModels` now treats the
   daemon-pinned entry as primary for exact known identities while preserving
   discovered availability, unknown-model passthrough, and installed-cache
   fallback metadata. The regression creates a private `CODEX_HOME` fixture
   whose `gpt-5.6-sol` label, default effort, effort set, and context window
   conflict with the pinned entry; the projection must return the pinned
   `GPT-5.6-Sol`, `medium`, five-effort contract while keeping an unknown
   gateway model selectable.
2. **Canonical Calendar server:** the Calendar store now exports pure
   current-server selectors. The route consumes `CurrentServerProvider` for
   rows, mutations, and notification synchronization; it clears selected,
   editor, and error state when that key changes. A screenshot-only override
   keeps the gated fixture deterministic without creating a production
   fallback. Reducer-level tests prove exact rebinding and empty results for a
   missing or null current key.
3. **Server event subscription teardown:** `Server` now owns one idempotent
   Work/Calendar/Brain unsubscribe operation, deferred before listener
   acquisition in `RunWithReady`. The broadcaster no longer owns only a
   subset of those resources. Tests use real stores and require every
   subscription channel to close after normal cancellation and after
   `net.Listen` failure.
4. **Repository residue:** the unreferenced 645,565-byte
   `app/ghostty-web-0.4.0.tgz` archive was deleted. Python `__pycache__`
   directories and `*.py[cod]` are ignored; caches created during verification
   were removed after the tests.
5. **CI vet gate:** the daemon CI job now runs `go vet ./...` between Go tests
   and build. The existing workflow contract test asserts all three commands
   and their order.

The non-document implementation delta is 266 text insertions and 52 text
deletions, plus the 645,565-byte binary deletion. Most insertions are behavior
tests for the two lifecycle changes and the metadata/current-server contracts.

## Final verification

| Check | Final result |
| --- | --- |
| Targeted `TestAccountCodexProjectionAddsKnownEffectMetadataWithoutGatingUnknownModels` | Passes against the hermetic conflicting installed-cache fixture. |
| `cd daemon && go test ./... -count=1` | Passes for every package; the baseline's sole red test is closed. |
| `cd daemon && go test -race ./modelprofiles ./server ./work ./calendar ./brain -count=1` | Passes. This covers all daemon packages directly changed plus the adjacent Work, Calendar, and Brain subscription owners. |
| `cd daemon && go vet ./...` | Passes. |
| `cd daemon && go build ./...` | Passes. |
| `cd app && bun test` | 1,117 pass, 4 explicit isolation/performance skips, 0 fail; 1,121 tests across 172 files. The three added passing tests are two Calendar projection cases and the strengthened CI workflow contract. |
| `cd app && bunx tsc --noEmit` | Passes. |
| `cd app && bunx expo export --platform android --output-dir "$ZEN_BUILD_TMPDIR/zen-health-android"` | Passes; 2,656 modules bundled. |
| `cd app && bunx expo export --platform ios --output-dir "$ZEN_BUILD_TMPDIR/zen-health-ios"` | Passes; 2,581 modules bundled. |
| `python3 -m unittest discover -s scripts/tests -p 'test_*.py'` | 23 pass. |
| `bun run installer:test` | 12 pass. |
| `./scripts/verify-libghostty.sh --contract` | Passes. |
| `bun run app:doctor` | Still fails 2 of 21 checks for the documented Hermes regression and 22 Expo package mismatches. No warning was suppressed and no dependency was changed. |
| `git diff --check` | Passes. |
| Route residual checks | No `*.test.*` / `*.spec.*` file and no `bun:test` import exists under `app/app`. |
| Archive/cache hygiene | No non-audit reference to the deleted archive; ignore checks match both Python cache locations; the generated caches and both Expo export directories were removed. |
| Secret scan | `gitleaks` is unavailable on this host. A no-output diff scan for private-key headers, AWS keys, OpenAI/GitHub token formats, and quoted credential assignments found no match. |
| Isolation checks | The diff contains no excluded Skills/Plugins path. Neither `daemon/modelprofiles/model_catalog_contract_test.go` nor `inbox/` is present in this worktree. |

No repository lint command exists, so there is no honest lint result to claim.
Adding a lint toolchain remains EH-09 rather than being hidden by an ad hoc
one-off command.

## Remaining risk and follow-up

- **Expo/Hermes (P1):** the known memory regression and SDK drift remain the
  only reproducible health-command red signal. Fix them as one coordinated SDK
  57 patch update with Android/iOS native and device evidence.
- **Current-server convergence (P1):** Stats and Sessions still contain broad
  multi-server workflows. Calendar is now bounded, but the remaining paths
  need a dedicated behavior decision and cross-screen switch tests.
- **Work watcher teardown (P1):** per-path debounce timers and the watcher
  goroutine are not joined by `Store.Close`. The observed post-close window is
  bounded, but a correct synchronized stop-and-join owner remains necessary.
- **Maintainability (P2):** no lint owner exists, and broad modules remain
  review hotspots. Neither is justification for unscoped formatting or file
  splitting.
- **Retained compatibility:** Brain durable migration, legacy timeline
  normalization, Pairing V1/V2, unknown-model passthrough, and WebSocket
  snapshot/reconnect fallback remain reachable and tested. They were not
  removed.

All session-owned test/export processes completed. Generated Expo outputs and
Python caches were cleaned after verification. Integration remains a Brain
review decision; this branch is not merged into `main` by this audit.
