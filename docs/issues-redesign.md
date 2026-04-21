# Issues Redesign — File-First, Agent-Native

Date: 2026-04-21
Status: Design approved, ready for implementation plan

## Goal

Replace the current Linear-style issue tracker (task + run + comment with status, priority, labels, projects, due dates, 14+ picker components) with a file-first, minimalist system. Each issue is one Markdown file. The filesystem is canonical truth. Agents read, edit, and complete issues by editing the Markdown directly. The app is a thin viewer/editor around the file.

## Non-Goals

- Migration from existing `tasks.json` / `runs.json` data. The old system is deleted outright; existing data is discarded.
- Backwards compatibility with the old WebSocket message shape.
- Multi-agent coordination on a single issue (one issue = one primary agent).
- Real-time collaborative editing between app and agent (mtime-based conflict detection is sufficient).
- Rich-text or WYSIWYG editing (plain Markdown text, no contenteditable chip rendering).

## Filesystem Layout

```
~/.zen/
└── issues/
    ├── inbox/                          # unassigned issues (no project.toml required)
    │   └── 2026-04-21-fix-push.md
    ├── zen/
    │   ├── project.toml
    │   └── 2026-04-21-redesign-issues.md
    └── homelab/
        ├── project.toml
        └── 2026-04-20-tune-nginx.md
```

Rules:
- `~/.zen/issues/<project>/` is the root for issues of a project.
- `inbox` is a reserved project name. It has no `project.toml` and works out of the box for quick capture.
- Filename carries no semantics. Humans use `YYYY-MM-DD-slug.md` by convention; the daemon identifies issues by the `id` in frontmatter, so renaming or moving the file is safe.
- Creating a new project = creating a new subdirectory under `issues/` with a `project.toml`.

## Issue File Format

```markdown
---
id: 01HZ5K8J9X                          # ULID, stable identity
created: 2026-04-21T14:32:15+08:00
done:                                   # empty = active; ISO8601 timestamp = completed
---
# Fix push notification delivery

@claude iOS stops receiving pushes after 5 minutes in background.
Check whether the Expo token has expired in the daemon push flow.
```

Frontmatter fields (minimal, three user-facing):
- `id` (string, ULID): generated on creation, never changes. Used by the daemon as the primary key.
- `created` (RFC3339 timestamp): set on creation, never changes.
- `done` (RFC3339 timestamp | empty): empty means active. A timestamp means completed. This is the only completion signal. Clearing the field sends the issue back to active. The parser treats YAML `null`, empty string, and a missing key as equivalent "active".

Runtime fields appended by the daemon after dispatch (same frontmatter block, visible to the user):
- `dispatched` (RFC3339 timestamp): presence indicates the issue has been sent to an agent.
- `agent_session` (string): the tmux session id handling the issue.

Body: free-form Markdown. The first `#` heading (or the first non-empty line) is the title. Mentions (`@claude`, `@claude#session-id`) are plain text. Agents edit the body freely — no machine-managed regions, no section conventions. We trust the agent to be a reasonable Markdown editor.

## Project Config (`project.toml`)

```toml
name = "zen"
cwd = "/home/daoleno/workspace/zen"     # default cwd for dispatched agents
executor = "claude"                     # default executor when no @mention present
```

Minimal. The daemon reads this on load and on file change. `inbox/` skips this file and requires either an explicit `@mention` (to pick an executor) or the global default from `daemon.toml`.

## Mentions

Syntax:
- `@<role>` — any idle agent of that role; spawn a new one if none is available. Example: `@claude`.
- `@<role>#<session>` — specific existing tmux session. Example: `@claude#zen-claude-3`.

Extraction regex (applied after escaping email-like patterns): `(?:^|\s)@([a-z][a-z0-9-]*)(?:#([a-z0-9-]+))?\b`. Emails like `user@host.com` do not match because they are preceded by a non-space character.

The first mention in the body is the primary; later mentions are text only. A single issue dispatches to one agent.

## Components

### Daemon — kept

- `watcher/` — tmux session discovery and agent state classification. Unchanged.
- `server/` — HTTP + WebSocket transport, auth, push dispatch. Trim message handlers.
- `push/` — Expo push client. Unchanged.

### Daemon — deleted

- `task/` package in its entirety: `types.go`, `store.go`, `run.go`, related tests.
- Persisted files: `tasks.json`, `runs.json`, `meta.json`. Deleted on first run of new daemon.
- `.zen/task.md` per-worktree writer.
- WebSocket messages: `create_task`, `update_task`, `list_tasks`, `list_runs`, `create_run`, `delegate_task`, `add_task_comment`, `delete_task`, and their broadcast events (`task_created`, `task_updated`, `run_created`, `run_updated`).

### Daemon — new

- `issue/` package:
  - `parser.go`: frontmatter + body + mention parsing. Uses `gopkg.in/yaml.v3` for frontmatter.
  - `store.go`: directory scan, fsnotify watcher with 200ms debounce, in-memory snapshot `map[path]*Issue`, atomic writes (temp file + `os.Rename`), mtime-based conflict check.
  - `dispatch.go`: picks an idle session matching executor + cwd, or spawns a new session via the existing watcher API. Writes initial prompt via tmux `send-keys`. Appends `dispatched` + `agent_session` to frontmatter.
- `server/` — new messages:
  - `list_issues` → current snapshot, grouped by project.
  - `get_issue { id }` → single issue.
  - `write_issue { id?, project, path?, frontmatter, body, base_mtime? }` → upsert; returns new mtime or conflict with the current content.
  - `send_issue { id }` → trigger dispatch. Returns error if already dispatched or no executor available.
  - `redispatch_issue { id }` → clear `dispatched` + `agent_session` from frontmatter and run dispatch again. Separate endpoint so redispatch is an explicit user action.
  - `delete_issue { id }` → remove file.
  - `list_executors` → executor roles from `daemon.toml`.
- `server/` — new events:
  - `issue_changed { path, issue }` — full issue payload on any file change.
  - `issue_deleted { path, id }`.
  - `issues_snapshot { issues }` — sent once on client connect, replaces the client cache.

### App — deleted

- `app/(tabs)/issues.tsx` (current implementation).
- `app/issue/[id].tsx` (current implementation).
- All of `components/issue/*` (14 files: `CreateIssueSheet`, `StatusPicker`, `PriorityPicker`, `DueDatePicker`, `ProjectEditorSheet`, `AssignIssueSheet`, `DelegateRunSheet`, `IssueRow`, etc.).
- Redux `tasks` slice and related selectors/actions.

### App — new

- `app/(tabs)/issues.tsx` — list grouped by project, two sections per project: **Active** and **Done**. Top-right `+` creates a new issue (picks project, opens editor with empty body).
- `app/issue/[id].tsx` — full-screen Markdown editor + mention picker + Send button.
- `components/issue/MarkdownEditor.tsx` — plain `<TextInput multiline>` with scan-on-change to detect the active mention prefix and surface the picker.
- `components/issue/MentionPicker.tsx` — floating list. React Native does not expose a pixel-precise caret position for `TextInput`, so the picker is anchored below the editor (or above the keyboard on mobile) rather than at the caret. Shows `@role` entries first, then running sessions of the current project. Keyboard (`↑↓` / `Enter` / `Esc`) and touch selection.
- `components/issue/IssueRow.tsx` — list row: title (first `#` heading or first non-empty line), relative time, single-glyph status badge computed from frontmatter:
  - `done` non-empty → `✓` (Done section)
  - `done` empty + `dispatched` set → `▶︎` working
  - `done` empty + `dispatched` empty → `●` draft/active
- Redux `issues` slice: `{ byId: Record<string, Issue>, byProject: Record<string, string[]>, executors: string[] }`. `issue_changed` upserts; `issue_deleted` removes.

## Dispatch Flow

1. App sends `write_issue` to persist the current editor state, then sends `send_issue { id }`.
2. Daemon `dispatch.go`:
   - Loads issue, parses mentions.
   - If `dispatched` is already set, returns `{ error: "already_dispatched" }`. The app offers a Redispatch action (clears `dispatched` + re-sends).
   - Picks primary executor: first `@mention` > `project.toml.executor` > `daemon.toml.default_executor`.
   - Finds an idle tmux session in the same cwd matching the executor. If none, spawns a new session: `tmux new-session -d -c <cwd>` then runs the executor command from `daemon.toml`.
   - Sends initial prompt via `tmux send-keys`:
     ```
     Your task is at <absolute path>.
     Read it, do the work, and edit the file as you progress.
     When finished, set `done: <ISO8601 timestamp>` in the frontmatter.
     ```
   - Appends `dispatched` and `agent_session` to the issue frontmatter via the store (triggers `issue_changed`).
3. App re-renders with the new frontmatter. The Send button becomes a disabled "Working…" indicator; a "Redispatch" action appears in an overflow menu.

Failure modes (all return structured errors on the `send_issue` response and a toast in the app):
- `executor_not_configured`: the mentioned role is not in `daemon.toml`.
- `spawn_failed`: tmux command failed. Error text passed through.
- `issue_not_found`: file was deleted before dispatch.

## Watcher + Sync Flow

- `fsnotify` recursive watcher on `~/.zen/issues/`. Events debounced 200ms per path.
- On event, re-read file, re-parse, compare with snapshot, broadcast `issue_changed` with the full payload (or `issue_deleted`).
- On daemon startup, full rescan of `~/.zen/issues/`, rebuild in-memory snapshot. First-connected-client receives `issues_snapshot`.
- Full body is broadcast (no diff). Issues are small enough (single-KB Markdown) that diffing is unnecessary complexity.

Done detection: the watcher parses `done` frontmatter. Non-empty → Done section. Empty → Active section. Toggling is bidirectional — clearing `done` manually re-activates the issue.

### Conflict Handling

- App sends `base_mtime` (file mtime at read time) with every `write_issue`.
- Daemon checks current mtime against `base_mtime` before writing. Mismatch → returns `{ error: "conflict", current: <issue> }`.
- App shows "Remote changes — keep mine / use theirs" banner and lets the user decide. No auto-merge.
- Agent writes bypass the daemon (they hit disk directly), so agent updates always win atomically and surface to the app via `issue_changed`. If the user is actively editing, the app shows a non-destructive "remote updated" banner without overwriting the draft.

### Atomic Writes

Daemon uses `os.CreateTemp` in the target directory + `os.Rename` for atomicity on the same filesystem. Agent writes bypass the daemon (they edit the file directly); the `claude` and `codex` CLIs already use atomic write-then-rename via their edit tools. If an agent does write non-atomically, the fsnotify debounce (200ms) absorbs partial writes and the daemon re-reads until parse succeeds.

## Mention Picker UX

```
┌───────────────────────────┐
│ @claude                   │  ← role, always listed
│ @codex                    │  ← role, always listed
│ ─────────────────────     │
│ zen-claude-3  · zen       │  ← running session in current project
│ home-codex-1  · homelab   │  ← (only if same project; otherwise hidden)
└───────────────────────────┘
```

- Trigger: `@` at start of line or after whitespace.
- Filter: characters after `@` filter the list by prefix.
- Confirm: `Enter` / `Tab` / tap inserts the mention as plain text (`@claude` or `@claude#zen-claude-3`).
- Dismiss: `Esc` / blur / leading space without selection.
- Only sessions from the current issue's project are shown to avoid cross-project noise.
- Mentions render as plain text in the editor. Only the read-only view (not the edit view) renders them as visual chips.

Data source:
```ts
type MentionCandidate =
  | { kind: 'role'; name: string }
  | { kind: 'session'; role: string; sessionId: string; project: string };
```

## Executor Configuration

Extends the existing daemon config (`daemon.toml` or whatever the current daemon uses) with a `default_executor` string and an `[[executors]]` table:

```toml
default_executor = "claude"

[[executors]]
name = "claude"
command = "claude"        # executed via tmux when spawning a new session

[[executors]]
name = "codex"
command = "codex"
```

App fetches executor roles via `list_executors` on connect and caches them. Other existing daemon config (listen address, auth secret, etc.) is untouched.

## Deletion Plan — Atomic Rewrite

One PR that removes the old system and lands the new one in a single change. No intermediate two-daemons-running state.

Order within the PR (so reviewers can follow):
1. Delete `daemon/task/` package.
2. Delete old WebSocket handlers in `server/`.
3. Delete old persisted files references; keep deletion of actual `tasks.json`/`runs.json` files as a daemon startup step (log warning, remove them).
4. Delete app `components/issue/*`, `app/(tabs)/issues.tsx`, `app/issue/[id].tsx`, Redux `tasks` slice.
5. Add daemon `issue/` package and new WebSocket handlers.
6. Add app `issues` slice, editor, picker, row.
7. Add tests alongside each new module.

No migration script. Existing users discard their old data.

## Testing Strategy

### Daemon (Go)

- `issue/parser_test.go`:
  - Frontmatter parse: `done` empty / timestamp / malformed.
  - Body: title extraction from `#` heading or first non-empty line.
  - Mentions: `@claude`, `@claude#session-1`, email false-positives.
- `issue/store_test.go`:
  - Uses `t.TempDir()`. Create / read / update / delete round-trip.
  - fsnotify debounce: multiple writes within 200ms → one event.
  - Mtime conflict: write with stale `base_mtime` → returns `conflict`.
  - Rescan on startup produces the same snapshot as a continuously-running daemon.
- `issue/dispatch_test.go`:
  - Mock watcher/registry.
  - Idle session reuse vs spawn new.
  - Missing executor → structured error.
  - Redispatch clears `dispatched` field.
- `server/` integration:
  - In-process WebSocket client runs `write_issue` → `send_issue` → receives `issue_changed` with `dispatched` populated.

### App (TypeScript)

- `MentionPicker.test.tsx` (Jest + React Native Testing Library): `@` opens picker, typing filters, `Enter` inserts, `Esc` closes.
- `MarkdownEditor.test.tsx`: mention regex matches correctly; conflict banner renders on mtime mismatch.
- Redux `issues` slice: upsert / remove / group-by-project selectors.
- Manual E2E script (`docs/e2e-issues.md`):
  1. Create issue with `@claude`. Tap Send.
  2. Verify `dispatched` appears in frontmatter.
  3. Verify agent session appears in `tmux ls`.
  4. Have agent set `done: <ts>` — issue moves from Active to Done in the app.
  5. Clear `done` — issue returns to Active.

### Verification Gate

- `cd daemon && go test ./...` all pass.
- `cd app && npx expo export --platform android` completes.
- Manual E2E script completes end-to-end.

## Deferred / Not in Scope

- Multi-agent coordination on one issue.
- Issue archiving / move-to-trash semantics (deletion is permanent).
- Attachments (images, files) — can be added later as a body convention (Markdown image links).
- Search UI in the app (defer until we feel friction).
- Issue templates.
- Auto-prune of old Done issues.

## Open Questions

None at approval time. Implementation may surface details; flag them in the implementation plan before coding.
