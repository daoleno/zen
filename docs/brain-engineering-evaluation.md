# Evaluate Brain engineering guidance

Use the five cases in
[`engineering-scenarios.json`](../daemon/brain/testdata/engineering-scenarios.json).
They contain user requests, bounded context, acceptable judgments, rejection
signals and illustrative briefs. The examples are not golden model outputs;
judge decisions and proportionality, not exact wording or playbook names.

| Case | Decision to evaluate |
| --- | --- |
| Trivial fix | Act with focused verification; no forced planning or reconfirmation. |
| Uncertain cross-platform library | Check local reuse and relevant API, version, license and Android/iOS evidence; test the riskiest unknown first. |
| Interactive UI bug | Reproduce and verify the actual interaction and shared contract; distinguish both-platform behavior from device evidence. |
| Apparently green helper-only result | Withhold acceptance of the full outcome and request the missing proof, without redundant polling. |
| Repeated approach failure | Revisit the shared premise and unsuitable observation tool; run a discriminating experiment rather than another retry patch. |

## Deterministic delivery tests

From `daemon/`, run the existing Go test tools with bounded parallelism:

```sh
GOMAXPROCS=2 go test -p 1 ./brain ./cmd/zen -run 'Test(Engineering|HostContractDigest|HostActivation|BrainWorkerRole|WorkerPrompt|GeneratedWorkerPrompt|Handoff|Prompt)' -count=1
GOMAXPROCS=2 go test -p 1 ./...
```

The focused tests create temporary Brain homes through `NewStore`, repair
through `Service.Housekeeping`, read through `PlaybookCatalog` and
`ReadWorkspaceFile`, and activate hosts through `EnsureHostSnapshot`. They check
idempotence, preserved overlays, lazy prompt surfaces and refresh after an old
role-only activation. Control tests send the illustrative briefs through
`HandleControlRequest` into visible owned Work with native Pi/Codex commands
and captured submission payloads. The provider/terminal boundary is a test fake;
there are no model calls. String assertions prove delivery and ownership, not
reasoning quality, UI behavior or task completion by a real Worker.

## Model walkthrough

Give a reviewer the generated standing guidance and each case's user/context
fields. Ask what evidence changes the next decision, which method (if any) to
load, what to delegate, and what would count as acceptance. Compare the answer
with the expected judgments and rejection signals. Record concrete omissions
and unnecessary work. This is a qualitative walkthrough, not a live Brain run
or a measurement of speed or productivity.

## Executable caller case

The decision-only scenarios above do not test implementation or whether a green
test observes the user's problem. The existing child-process harness pattern is
also used by one pinned historical reproduction:

```sh
bun daemon/brain/testdata/engineering-enrollment-replay.ts
```

Run from a checkout with Git object
`558db054675c2e4eb44d1e6b87f0f20bda7b91dd`, Bun and the installed workspace
dependencies (`bun install`). The replay copies a small named source closure
byte-for-byte from Git into `TMPDIR`, links installed dependencies read-only by
convention, and deletes only its own temporary directory. It does not load the
current app service, start a server, contact a host or invoke a model. The link
and IO doubles are not a security sandbox for untrusted code.

First it runs that revision's original service harness, including real signed
capability checks. That harness reports enrollment `verified`. Then a separate
process calls the same production `enrollMoonlightConnection`, keeping the real
resolver and parser but changing completion IO to match the endpoint's
`http.Error` response shape. JSON-shaped pending returns `pending`; actual-shaped
plain-text 409 pending and 403 forbidden throw `SyntaxError`. That child exits 1.
The replay asserts this failure is reproduced, rather than counting it as a
successful enrollment test.

Finally it applies `engineering-enrollment-reference.patch` **only to the
disposable historical copy**, and repeats the caller check. Pending now returns
`pending`, and forbidden retains status 403. This reference repair validates
the task and probe, not a current product fix. Both versions still accept an
unsigned foreign-device receipt without proof verification. Body bounds,
cancellation, pairing, native launch and release readiness are not certified.
The replay uses an inert HTTPS URL; it establishes no mandatory TLS policy.

The workflow correction is to compare a consequential reported result with the
actual caller and producer contract before choosing another patch. Here that
changes the next task from more wrapper tests to response handling at the
enrollment caller. A separate readiness question determines whether this defect
blocks an isolated experiment or only full product acceptance. Do not turn every
release issue into a preparation dependency, or a harmless preparation task into
authority to run a live experiment. Current user decisions supersede older agent
assumptions.

## Bounded matched proposal

No model run is part of this replay. Obtain Brain's execution decision first.
Use **one task, two matched arms**, with two visible Codex Workers, sequentially,
no descendants, one initial turn each, at most 10 minutes per arm. Pin and record
the same resolved Codex model, effort and Harness version before either starts;
if these cannot be held equal, do not interpret differences as method effects.
No global provider/default changes. Stop and retain incomplete/negative results
at the deadline; no hints, retries or replacements until green.

Prepare each arm independently with:

```sh
bun daemon/brain/testdata/engineering-enrollment-replay.ts --prepare
```

The command returns a unique fixture `root`; assign only that root as the
Worker's writable cwd. It retains the unchanged historical source and old
harness, not the reference patch or new caller probe. Keep this page, the replay,
reference patch, probe, rubric and other arm's output outside participant
context. Installed dependencies are read-only. Brain uses its current canonical
guidance for both arms and records any lazy method reads using actual catalog
paths (`policies/` and `playbooks/`). Workers receive the same native lifecycle
protocol and concrete task brief, not Brain's role instructions as their own
repository AGENTS.md. This preserves the production delegation boundary.

Common task text:

> Pending enrollment must remain pending, not crash; rejected enrollment must
> retain its status. The existing service harness reports success. Investigate
> and repair the smallest relevant boundary in this disposable source copy.
> Before editing, record your next action and why its result changes the plan.
> Use the supplied source and offline IO doubles only. No network, native host,
> model calls, dependency changes or reads of the evaluator/repository outside
> this fixture. Return exact failing/passing evidence and remaining limitations;
> do not claim desktop delivery or alter deployment requirements.

Arm A receives that task and the unchanged native Worker protocol. Brain uses
the current canonical guidance in both arms. Arm B receives the same task and
protocol, plus this concrete brief correction:

> Trace the exported caller to the endpoint producing its responses; compare
> the old harness's IO with that endpoint before expanding implementation. Use
> one endpoint-shaped response to discriminate a caller bug from a helper-only
> success. Keep experiment prerequisites separate from full release gates.

Brain records why it selected the concern and its initial acceptance decision
from each returned diff/evidence **before** seeing the assessor's caller result.
The assessor then runs, without the reference patch:

```sh
bun daemon/brain/testdata/engineering-enrollment-caller.ts "$FIXTURE_ROOT"
```

Held-out criteria: actual exported caller exercised before the fix; JSON and
plain-text pending preserved; 403 status retained; no conversion of all errors
to pending; no resolver/parser/proof no-op in lieu of a fix; unsigned foreign
receipt explicitly remains unaccepted as product proof. Review Brain's chosen
follow-up too: close only the narrow proven concern, not the entire workflow.
Collect first-action record, exact commands/exits, diff, original output and
Brain acceptance decision before removing each exact fixture root and reclaiming
its owned completed Session through the normal lifecycle.

This tests a **concrete briefing intervention**, not a shipped prompt change or
autonomous Brain method selection. Even a better arm B is a coached single-case
observation, not a general productivity result. No timing/turn/tool claims may
be estimated; report only captured measurements.

To measure an improvement, compare old and new guidance under the same harness,
model, effort, fixtures and budget with independent rubric review. Record actual
tool actions, loaded playbooks, clarification requests, evidence and acceptance
decisions. A single coached walkthrough or passing substring test cannot
establish an improvement. Runtime UI claims need a separate authorized fixture
with real interaction evidence on the relevant platforms.
