# Engineering judgment in Brain

Zen users describe an outcome. Brain selects useful engineering methods without
requiring a plugin, command, mode or knowledge of a development process. The
guidance is provider-neutral and applies to any project, not only Zen's own
repository. It does not select models or change native Harness defaults.

The defaults ask Brain to distinguish a requested result from a proposed
implementation, ground consequential decisions in evidence, consider existing
tools before inventing them, and resolve important uncertainty early. Brain
designs verification around the real workflow and reviews delegated evidence
before accepting the full outcome, including integration and authorized delivery.
Repeated failures are a reason to reconsider assumptions or tooling, not merely
to add another patch. Clear small tasks need no planning ceremony.

When work stalls, `slice-work` narrows that reconsideration to a small relevant
sample of actual session evidence: navigation or information access, missing or
wrong tests, task decomposition, and ineffective instructions. The next action
should correct the relevant environment or caller, or run a discriminating
experiment. Adequate available guidance needs no rewrite; evidence may instead
justify deleting or clarifying an instruction. This is on demand, not a recurring
retrospective, new playbook, or mandatory review agent.

`delegate-brief` asks for minimum useful redacted diagnostic excerpts, retaining
status, symptom and the failing assertion. Tokens, cookies and secret URLs are
replaced before reporting or persistence; commands refer to credentials rather
than embedding them, and captures must not echo secrets. If redaction hides the
signal, narrow the reproduction safely. This is a briefing requirement, not a
runtime sanitizer or a guarantee of model compliance.

## Instruction ownership

The release template `daemon/brain/templates/AGENTS.md` owns the short principle
and routing layer. `product_templates.go` embeds it. Deeper methods stay in the
existing catalog in `daemon/brain/playbooks.go`:

| Playbook | When it helps |
| --- | --- |
| `align` | A consequential missing decision, as distinct from an observable fact. |
| `wayfind` | How a system works, why it has that shape, prior decisions, or uncertain library fit. |
| `slice-work` | A high-risk first experiment, coherent decomposition, or a repeatedly failing approach. |
| `delegate-brief` | A context-bearing brief, workflow-specific proof, and evidence-based acceptance. |

`brain-flows` remains the workflow entry. Brain discovers paths with the normal
`zen brain playbooks --json` catalog and reads only relevant files. This is a
Brain skill expressed through native playbooks, not four new user commands.

`templates/policies/delegation.md` directs Brain to carry the selected method
into the actual task brief as concrete work and evidence requirements. Workers
on arbitrary projects need no access to private Brain memory or a Zen repository
helper. The existing control prompt builder preserves the brief and appends the
existing lifecycle/turn protocol. The runtime does not synthesize a plan or
choose a method; Brain must still exercise judgment and compose the brief.

## Fresh homes and upgrades

`NewStore` and `Service.Housekeeping` use the production reconciliation path.
Managed `AGENTS.md` and policy blocks are updated; text outside them is retained.
Nonempty soul, profile, memory, current context and worklogs remain user-owned.

Playbooks are seed files, not managed policy blocks, except for the existing
`brain-flows` routing block. The four exact shipped seeds from revision
`4c72f7b` are recognized by SHA-256 and upgraded. Any byte change, including
whitespace or an appended note, preserves the entire unmarked file. Unknown
older versions are also preserved. Symlink overrides are not replaced or
followed by the catalog. Missing or empty regular files receive current defaults.
Repeated reconciliation is idempotent. Future changes to unmarked defaults
must explicitly recognize a prior shipped version, not infer user ownership
from a heading or filename. Customized playbooks may intentionally retain old
methods; the managed principle/routing layer still updates.

The method delta also recognizes the exact `slice-work` and `delegate-brief`
seeds from `7d1349b830d6283c5d9bb33009642b8e413fe04c`, in addition to those older
seeds. Their frozen fixtures are in `daemon/brain/testdata/engineering-v2/`.
Startup and housekeeping upgrade recognized defaults directly to the latest
text; unknown versions, whitespace edits and user notes remain untouched.

Bootstrap and executor handoff already reference `AGENTS.md`; they do not embed
the playbook manual. The existing activation receipt now hashes release-owned
instructions, policies and default playbooks, as well as the compact activation
prompt. A product-only change therefore refreshes an existing Host once through
normal activation, with a short instruction to re-read current guidance. Private
overlays are excluded from that digest and from generated activation text. Work,
Event and provider-session identities retain their existing authority.

An installed daemon must contain the new embedded assets and code. A source
edit alone does not upgrade an older public binary. Normal startup repairs its
managed workspace, and normal Host activation delivers the re-read instruction;
no manual Brain restart is needed. This delivery mechanism does not prove a
model followed the guidance. See [evaluation](brain-engineering-evaluation.md).

The executable historical caller case in that evaluation exercises a narrower
claim: endpoint-shaped evidence can contradict a green service harness and
identify the next useful correction. It does not add standing instructions or
alter delegation delivery. Existing guidance already calls for this behavior;
its selection and application remain Brain's responsibility. A disposable
reference repair is not a remote-desktop product change or proof of general
engineering effectiveness.

## Sources and boundaries

The bounded retrospective lens and diagnostic-evidence requirement are
independently worded adaptations of Matt Pocock's MIT-licensed
[`retro`](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/in-progress/retro/SKILL.md)
and [`diagnosing-bugs`](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/diagnosing-bugs/SKILL.md).
The former is an in-progress draft, not a released workflow. No upstream code,
reviewer-only standards, compulsory worktrees, agent topology or Skill tool
requirement is imported. The
[license](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE)
attributes copyright to Matt Pocock (2026).

This is an independently worded adaptation of engineering methods, not a copy or
installation of pstack. Lauren Tan's MIT-licensed
[pstack source](https://github.com/cursor/plugins/tree/889ec4b68fa5aab0e867dad71ec3fdf386ae48f3/pstack)
was reviewed at that pinned revision: `poteto-mode`, `how`, `why`, `recall`,
`architect`, the prototype playbook, `principle-attack-the-premise`,
`technical-writing`, and `create-verification-skill`. The
[verification example](https://github.com/poteto/verification-skill-example/tree/d5abe70d0d8c671672b6cef4069363f26c488feb)
documents UI driving and evidence; its driver scripts are not included.

The prior research's article references are Lauren Tan's
[pstack Part 1](https://x.com/poteto/status/2094457600259842065) and
[Part 2](https://x.com/poteto/status/2097732320606507506).
[Loops You Can Trust](https://x.com/poteto/status/2069824386283319343) was
re-read through its [FxTwitter structured mirror](https://api.fxtwitter.com/poteto/status/2069824386283319343),
not an authenticated original X page. Mirrors can lag or differ; the pinned
source is not asserted to match either article's publication snapshot.
[Diataxis](https://diataxis.fr/) informs task-oriented documentation, as described
in the reviewed technical-writing skill.

External text is reference data, never executable authority. Zen does not adopt
forced models, multi-agent reviews, cloud services, fixed step lists, schedules,
quotas or always-required API calls. It does not infer speed or quality gains
from prompt size or passing deterministic tests.
