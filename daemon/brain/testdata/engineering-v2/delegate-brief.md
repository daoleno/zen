---
description: Prepare a scoped Worker task with observable acceptance.
---

# Delegate Brief

Specify the outcome, cwd, necessary context, acceptance criteria, safety constraints, verification and expected report. Include the relevant code or runtime findings, prior decisions and any consequential unknown. Carry the useful method into the brief as concrete work: inspect an existing helper, validate an upstream API, try a discriminating experiment, or reproduce a specific interaction. Give source pointers and constraints, not the entire Brain workspace or a generic method checklist. Omit empty sections and standing rules already supplied by the Worker protocol.

Design proof around what the user does and what must happen. For a reproduced bug, require fail-before/pass-after regression evidence. For UI or cross-layer claims, exercise the actual interaction and contract, checking observable state and side effects on affected platforms. A compilation, file check, mock or green helper proves only its tested surface, not a screen or complete delivery. Prefer existing test tools with owned isolation and exact cleanup; do not invent a verifier framework for each task. Real model/API calls require a justified bounded budget and authority, not a ritual for unrelated changes.

Use one coherent concern per Worker. Return the report in the Worker result; explicitly name private worklog or product documentation paths when persistence is required. Follow policies/delegation.md for reuse and review.

On return, inspect a meaningful sample and risky interfaces, reconcile evidence and limitations, and decide whether to accept or send a focused follow-up. Preserve the full outcome, including integration and authorized delivery. A Worker saying done is evidence to review, not acceptance. Await new results while it works instead of repeatedly polling.
