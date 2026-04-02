# Notifications

This document defines the OSS-core notification policy for zen.

## Goal

zen notifications are not a progress feed.
They should only interrupt the user when one of two things is true:

1. The agent needs attention now.
2. The agent finished while the user was away from that session.

That keeps the system simple, predictable, and low-noise.

## Current Product Policy

zen should notify on these agent states:

- `blocked`
- `failed`
- `done`

zen should stay silent for these states and events:

- `running`
- `unknown`
- reconnect / disconnect / websocket chatter
- periodic refreshes with no meaningful state change

## Suppression Rule

There is one primary suppression rule:

- If the exact agent session is currently open and focused, do not notify.

Everything else should stay straightforward:

- If the app is foregrounded but the user is looking at another session, a local notification is allowed.
- If the app is backgrounded or the user left the app, the daemon may send a remote push.
- We do not need reminder ladders, digests, or a manager layer in OSS core right now.

## Notification Copy

### Blocked

Use when the agent cannot continue without the user.

- Title: `Input needed · <label>`
- Body: cleaned summary, or `Open zen to respond.`
- Priority: high

### Failed

Use when the session ended in a failure state that likely needs inspection.

- Title: `Task failed · <label>`
- Body: cleaned summary, or `Open zen to inspect the last output.`
- Priority: high

### Done

Use when the session finished and the user is not currently in that session.

- Title: `Finished · <label>`
- Body: cleaned summary, or `Session finished.`
- Priority: default

Important:

- `done` should use neutral wording.
- Do not say `completed successfully`.
- Today the classifier can tell that the session finished, but not that the underlying business task fully succeeded.

## Content Rules

### Labels

Prefer a short user-facing label, not raw tmux or shell names.

Preferred order when available:

1. Explicit user alias
2. Project name
3. Cleaned agent name
4. Agent ID as fallback

Examples:

- good: `backend-api`
- good: `release-cut`
- bad: `./bin/zen-daemon (main:7)`
- bad: `server_mnguamzs_a1sz5a`

### Body text

The body should explain why the notification matters.

Rules:

- Prefer the most actionable summary we have.
- Strip timestamps.
- Strip shell noise when it is not the actual reason.
- Keep it concise, roughly within 100 to 120 characters.
- Avoid echoing internal IDs unless that is the only identifier available.

## Implementation Split

Current OSS-core behavior is intentionally simple:

- The mobile app schedules local notifications for state transitions while the app is active, but only when the selected session is not the one that changed.
- The daemon sends remote push notifications when a push token is registered and no active viewer is attached to that exact session.
- Both sides should keep the same title and summary-cleaning conventions.

This is enough for a good default experience without introducing a control plane or a more complex notification manager.

## Future Work

If notification noise becomes a real problem later, the next upgrades should be:

1. de-duplication by `agent_id` + `state_version` + reason
2. multi-device push registrations
3. richer classifier reasons for better summaries
4. optional per-run `notify_on_completion`

Those are later improvements, not prerequisites for shipping the OSS core.
