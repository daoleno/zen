# Agent-Native Task Model for zen

This document proposes how `zen` can evolve from a session-first mobile control plane into a task-native control plane inspired by the best parts of Linear's recent agent work.

It is intentionally opinionated and anchored in the current codebase:

- tasks are persisted in `daemon/task/store.go`
- delegation currently happens in `daemon/server/server.go`
- tmux sessions are observed through `daemon/watcher/watcher.go`
- the mobile app already has both `agents` and `tasks` stores

The goal is not to copy Linear's product surface area. The goal is to absorb the design principles that matter and apply them to `zen`'s distinct shape: self-hosted, mobile-native, terminal-attached agent operations.

## What Linear Gets Right

Linear's recent agent-native work is useful because it is not "AI features inside an issue tracker." It is a task system designed so humans and agents can both participate safely.

The principles worth borrowing are:

1. The task is the durable object. Chat is not.
2. The human remains accountable. The agent is a delegate, not the owner.
3. Agent work is modeled as a visible state machine, not an opaque transcript.
4. Context should live in the system, not be rebuilt manually in every prompt.
5. Autonomy should expand gradually: suggest, delegate, auto-apply, automate.
6. Agents should work close to the source of the problem, not after context is copied into another tool.

## Current Shape in zen

Today `zen` already has the beginnings of a task-native model:

- `Task` exists as a first-class object.
- a task can carry `skill_id`, `project_id`, `cwd`, `agent_id`, and `agent_status`
- the app can `Create` or `Create & Delegate`
- daemon-side watcher events can push task status forward automatically

That is a strong starting point.

The main limitation is structural:

- a task and an execution attempt are collapsed into the same record
- `agent_id` is both the current executor and the only durable link to runtime
- retries overwrite history
- task status and agent status are mixed together
- the product is still largely session-first at the UI level

In other words, `zen` already has tasks, but it does not yet have a real execution model.

## Core Reframe

The key change is:

- `Task` is the durable unit of work
- `AgentSession` is a live agent thread that may exist with or without a task
- `Run` is a task-scoped execution attempt that may attach to an existing agent session
- `TerminalAttachment` is the mobile transport connection used to inspect or drive a live session

That split sounds small, but it changes the product.

Instead of:

- task -> current `agent_id`

Move to:

- task -> many runs
- run -> optional agent session
- agent session -> zero or many runs over time
- terminal attachment -> temporary mobile connection to a live agent session

This is the same conceptual move that makes Linear's design feel agent-native rather than chat-native, while still respecting `zen`'s runtime-first reality.

## Proposed Domain Model

### Why This Matters for zen

Unlike Linear, `zen` is attached to a real runtime environment.

That means not every live session starts from a task.

Examples:

- the user starts Claude or Codex manually on their laptop
- the user opens a tmux window directly in a terminal
- the user creates a session from the mobile app without creating a task first
- later, the user decides that session is important enough to formalize as a task

So the model cannot be:

- every session belongs to a task

It needs to be:

- tasks are optional
- sessions are optional
- runs are the bridge when task intent and live execution meet

### Task

`Task` remains the user-facing work item.

Suggested fields:

```go
type Task struct {
    ID              string
    Number          int
    Title           string
    Description     string
    Status          TaskStatus
    Priority        int
    Labels          []string
    ProjectID       string
    SkillID         string
    Cwd             string

    OwnerType       string // "human" | "agent"
    OwnerID         string
    SourceType      string // "manual" | "share" | "slack" | "web" | "voice" | "api"
    SourceSummary   string

    CurrentRunID    string
    LastRunStatus   string
    RunCount        int

    CreatedAt       time.Time
    UpdatedAt       time.Time
    CompletedAt     *time.Time
}
```

Important differences from the current model:

- `AgentID` moves off the task
- task status expresses business progress, not terminal/runtime state
- source metadata becomes first-class so context does not disappear

### AgentSession

`AgentSession` is the durable record of a real live agent thread discovered or created in the runtime environment.

This maps much more closely to what the watcher sees today.

```go
type AgentSession struct {
    ID                string
    Backend           string // "tmux", later maybe "pty", "ssh", "remote"
    RuntimeTargetID   string // tmux target or equivalent

    ExecutorKind      string // "claude" | "codex" | "custom"
    Origin            string // "desktop_manual" | "mobile_manual" | "task_delegate" | "api"

    Name              string
    Cwd               string
    Project           string
    Summary           string

    Status            SessionStatus
    LastOutputLines   []string
    LastHeartbeatAt   *time.Time
    ArchivedAt        *time.Time

    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

Important properties:

- an agent session can exist without a task
- an agent session can later be linked to a task
- an agent session may be reused for multiple task runs over time
- an agent session is the thing you open in the terminal view

This is a better fit for `zen` than pretending every runtime thread is task-derived.

### Run

`Run` is the bridge between a durable task and a live or queued execution attempt.

```go
type Run struct {
    ID                string
    TaskID            string
    AttemptNumber     int

    Status            RunStatus
    ExecutionMode     string // "spawn_new_session" | "attach_existing_session"
    ExecutorKind      string // "claude" | "codex" | "custom"
    ExecutorLabel     string
    RequestedBy       string // device/user id if available

    AgentSessionID    string
    BoundAt           *time.Time

    PromptSnapshot    string
    GuidanceSnapshot  GuidanceSnapshot
    SkillSnapshot     SkillSnapshot
    ContextSnapshot   []ContextRef

    StartedAt         *time.Time
    EndedAt           *time.Time
    LastHeartbeatAt   *time.Time

    Summary           string
    LastError         string
    WaitingReason     string
}
```

Why this matters:

- retries become visible and durable
- task history survives agent failure
- one task can be resumed, retried, or handed to a different executor
- a task can bind to an already-running session instead of always spawning a new one
- prompts, guidance, and context are snapshotted for debuggability

Recommended constraint:

- one agent session should have at most one active run at a time

That avoids ambiguity on mobile and keeps ownership clear. A long-lived agent session can still handle multiple tasks over its lifetime, but sequentially, not as overlapping active runs.

### TerminalAttachment

`TerminalAttachment` should stay implementation-level.

It exists so the mobile app can attach to live terminal I/O for an `AgentSession`.

```go
type TerminalAttachment struct {
    ID          string
    AgentSessionID string
    Backend     string
    OpenedBy    string
    LastSeenAt  time.Time
}
```

This means:

- a task detail screen can show "current run blocked" even if no terminal is attached
- the user can inspect an unlinked live session without forcing task creation first

### Activity

This is the second major missing object today.

Raw terminal output is evidence, but product UX needs structured activity.

```go
type Activity struct {
    ID          string
    TaskID      string
    RunID       string
    Kind        string // "delegated" | "started" | "note" | "action" | "question" | "blocked" | "artifact" | "completed" | "failed"
    Title       string
    Body        string
    Metadata    map[string]any
    CreatedAt   time.Time
}
```

This is where `zen` can become much stronger on mobile.

A phone should not force the user to parse a terminal transcript just to answer:

- what happened
- what needs my attention
- what changed
- what did the agent produce

### Artifact

Runs often produce durable outputs:

- branch name
- commit SHA
- diff summary
- test result
- patch file
- note
- screenshot
- URL

Treat those as first-class outputs instead of burying them in scrollback.

```go
type Artifact struct {
    ID          string
    TaskID      string
    RunID       string
    Kind        string // "branch" | "commit" | "diff" | "test" | "file" | "url" | "note"
    Title       string
    Value       string
    Preview     string
    Metadata    map[string]any
    CreatedAt   time.Time
}
```

### Guidance

The current guidance model is useful but too flat.

Right now guidance is effectively one global prompt prefix per server. That is a good start, but the next level should be scoped guidance:

```go
type GuidanceScope struct {
    ID           string
    ScopeType    string // "server" | "project" | "skill" | "task"
    ScopeID      string
    Preamble     string
    Constraints  []string
    UpdatedAt    time.Time
}
```

Prompt construction then becomes layered:

1. server guidance
2. project guidance
3. skill guidance
4. task-specific instructions
5. runtime context snapshot

This is much closer to how Linear thinks about system context: less manual prompt editing, more reusable operating context.

## State Machines

### Task Status

Task status should answer: "Where is the work overall?"

Recommended task states:

- `backlog`
- `todo`
- `in_progress`
- `blocked`
- `in_review`
- `done`
- `cancelled`

Task status is a product-level truth, not a mirror of terminal state.

Examples:

- if a run fails, the task probably goes back to `todo`, not `failed`
- if a run asks for input, the task may become `blocked`
- if a run produces a patch waiting for approval, the task may become `in_review`

### Run Status

Run status should answer: "What is this execution attempt doing right now?"

Recommended run states:

- `queued`
- `starting`
- `running`
- `waiting_input`
- `blocked`
- `failed`
- `completed`
- `cancelled`
- `superseded`

This split avoids the current ambiguity where `agent_status` sometimes means execution state and sometimes business state.

## Runtime Flow

### Current Flow

Today:

1. client sends `create_task`
2. client sends `delegate_task`
3. daemon builds prompt from guidance + skill + task text
4. daemon creates tmux session
5. daemon stores `task.agent_id`
6. watcher updates task status based on agent state changes

This works, but the runtime record is implicit and it assumes the main path is task -> agent.

### Proposed Flow

Recommended future flow:

1. watcher discovers or creates `agent_session` records for live sessions
2. client may create a `task`
3. client may create a `run` for that task
4. the run either:
   - spawns a new agent session
   - or attaches to an existing agent session
5. daemon snapshots prompt, guidance, and context into the run
6. daemon emits `run_created`
7. watcher updates the agent session state
8. server derives run state from the linked agent session plus explicit user actions
9. task status is derived from the active run plus user actions
10. structured activities and artifacts are appended throughout the run

That gives `zen` durable history, better notifications, and cleaner mobile UX without discarding the terminal control plane.

## Product Surfaces

Because not all live sessions are task-backed, the mobile product should not force everything into a task list.

Recommended shape:

### Tasks

This is the planning and delegation surface.

Actions:

- create task
- delegate to new session
- attach task to existing session
- inspect task timeline
- review artifacts

### Live Sessions

This is the runtime surface for all active agent sessions, whether task-backed or not.

Actions:

- inspect output
- send input
- terminate
- create task from session
- link session to existing task
- pin or ignore

### Attention Feed

This is the mobile-first top-level feed.

It can contain both:

- task-backed items needing attention
- unlinked sessions that are blocked, failed, or newly important

That is more honest than pretending every runtime event belongs to a task.

## API Rewrite

Because the product has not launched, do not preserve the current compatibility surface unless it is helping implementation speed.

Recommended clean API:

### Core list events

- `task_list`
- `agent_session_list`
- `run_list`
- `activity_list`
- `artifact_list`

### Core mutation events

- `create_task`
- `update_task`
- `delete_task`
- `create_run`
- `cancel_run`
- `link_run_to_session`
- `create_task_from_session`
- `link_session_to_task`

### Runtime events

- `run_created`
- `run_updated`
- `run_completed`
- `run_failed`
- `run_cancelled`
- `agent_session_created`
- `agent_session_updated`
- `agent_session_archived`
- `activity_appended`
- `artifact_created`

### Suggested clean cut

Because there is no launch migration burden yet:

- remove `task.agent_id`
- remove `task.agent_status`
- rename the current app-side `agents` view model to `agentSessions`
- make task rows read from `current_run` rather than from raw session linkage

Initially, activity can be generated from existing watcher events and explicit user actions:

- task delegated
- terminal attached
- state changed to blocked
- state changed to done
- approval sent
- retry requested

Even a shallow activity system will improve the mobile product immediately.

## UI Direction

### Do not make the app task-only

That would be wrong for `zen`, because part of its value is that it can see and control ambient sessions that started outside the app.

### Do not make the app session-only either

That throws away the planning, ownership, and review advantages of a task system.

### Recommended product split

- top-level attention feed mixes task-backed work and unlinked important sessions
- task detail becomes the semantic view of a piece of work
- live session detail remains the runtime control view
- terminal becomes a subview of a live session, not the only way to understand what is happening
- every live session screen should expose:
  - `Create task from session`
  - `Link to existing task`
- every task screen should expose:
  - `Delegate to new session`
  - `Attach to live session`

On mobile, this matters a lot. A phone is better for triage and judgment than for transcript archaeology.

## Scoped Guidance and Artifacts

Once runs exist, guidance and artifacts become much easier to reason about:

- project-level coding rules
- skill-level execution profiles
- task-level extra instructions
- artifact cards for commits, branches, tests, links, and notes

## Notification Model

Current notification policy is session-aware. The next step is to make it dual-aware:

- task-aware when work is task-backed
- session-aware when a live session matters but is not linked yet

Notify on:

- a task is blocked and needs a user decision
- a task enters review and has an artifact to inspect
- a run failed and the task returned to `todo`
- a task completed while the user was away
- an unlinked live session becomes blocked or failed and has not been explicitly ignored

Avoid notifying on:

- every run state change
- generic "agent is running"
- transcript noise

The notification payload should reference the task first when one exists. Otherwise it should reference the live session cleanly.

Good:

- `Input needed · ZEN-42`
- `Ready for review · ZEN-42`
- `Run failed · ZEN-42`

Less good:

- `Session blocked · backend-api (main:7)`

Good fallback for unlinked sessions:

- `Input needed · codex in zen`
- `Run failed · release shell`

This is directly aligned with the Linear principle that the user cares about work outcomes, not tool internals.

## What zen Should Copy, and What It Should Not

Copy:

- task-first modeling
- explicit execution state
- structured activity
- layered context and guidance
- human accountability with agent delegation
- gradual autonomy

Do not copy blindly:

- heavyweight issue-tracker hierarchy
- web-first interaction assumptions
- product surface area built for large org process
- the idea that chat should be the center of everything

`zen` has a different advantage:

- it is mobile-native
- it is close to live terminal operations
- it is self-hosted and infrastructure-light
- it can be the fastest "approve, redirect, retry, inspect" interface in the stack

The right move is not "become Linear on mobile."

The right move is:

- borrow Linear's task-native execution model
- keep `zen`'s strength as the fastest control plane for live agent work

## Concrete Recommendation for zen

If only one architectural change should happen next, it should be this:

Introduce `AgentSession` and `Run` as separate first-class objects, then make task delegation create a run that optionally binds to a session.

That one change unlocks:

- support for sessions that were started outside task creation
- retries without history loss
- task-aware notifications
- better mobile task detail screens
- structured activity feed
- multiple executors over time
- clean separation between business state and runtime state

It is the smallest change that moves `zen` from "remote terminal with task metadata" toward "agent-native task control plane" without denying that live sessions can exist before tasks.

## Suggested Near-Term Milestone

The most pragmatic near-term milestone is:

1. add `AgentSessionStore` backed by watcher-discovered sessions
2. add `RunStore`
3. remove `task.agent_id` and `task.agent_status`
4. add `current_run_id` and `last_run_status` to tasks
5. implement `create_run` with two modes:
   - spawn new session
   - attach existing session
6. emit both `agent_session_*` and `run_*` WebSocket events
7. add `Create task from session` and `Link to existing task` on live session screens
8. add a basic task detail timeline with generated activity items

That is enough to validate the product shift before investing in richer intake, artifacts, or broader workflow automation.
