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

## Optional live evaluation

Obtain Brain's execution decision before creating additional test Workers or
spending on model calls. A bounded proposal is five isolated visible evaluation
Workers, one per case, using the configured native executor without changing its
model or effort. Each gets only the generated guidance, its user/context fields,
and authorized synthetic project fixtures, never the expected judgments or
example brief. One decision/brief response per case is enough for a first pass;
it does not prove implementation quality. No network/API calls inside cases,
cloud agents or schedules are needed. Set a time/token budget and exact cleanup
ownership before launch.

To measure an improvement, compare old and new guidance under the same harness,
model, effort, fixtures and budget with independent rubric review. Record actual
tool actions, loaded playbooks, clarification requests, evidence and acceptance
decisions. A single coached walkthrough or passing substring test cannot
establish an improvement. Runtime UI claims need a separate authorized fixture
with real interaction evidence on the relevant platforms.
