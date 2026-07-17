# Notifications

This document defines the OSS-core notification policy for zen.

Calendar reminder notifications are covered separately in [calendar.md](calendar.md). They are explicit user commitments, so they are scheduled locally after sync and are not subject to the agent-state suppression rules below.

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

- If the app is foregrounded but the user is looking at another session, the daemon still attempts a remote push.
- If the app is backgrounded, suspended, or not running, the daemon uses the same remote-push path.
- We do not need reminder ladders, digests, or a manager layer in OSS core right now.

## Notification Copy

### Blocked

Use when the agent cannot continue without the user.

- Title: `<label> needs input`
- Body: cleaned summary, or `Waiting for your response.`
- Priority: high

### Failed

Use when the session ended in a failure state that likely needs inspection.

- Title: `<label> failed`
- Body: cleaned summary, or `Check the terminal for details.`
- Priority: high

### Done

Use when the session finished and the user is not currently in that session.

- Title: `<label> finished`
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
- bad: `./bin/zen (main:7)`
- bad: `server_mnguamzs_a1sz5a`

### Body text

The body should explain why the notification matters.

Rules:

- Prefer the most actionable summary we have.
- Strip timestamps.
- Strip shell noise when it is not the actual reason.
- Keep it concise, roughly within 100 to 120 characters.
- Avoid echoing internal IDs unless that is the only identifier available.

## Runtime ownership

Current OSS-core behavior is intentionally simple:

- The daemon is the only runtime lifecycle/result OS-alert producer. The app registers its Expo token, presents incoming pushes, and handles deep links; it does not mirror agent or scheduled-result state into local alerts.
- Ordinary delegated `blocked`, `failed`, and `done` transitions each trigger one best-effort daemon attempt unless that exact agent is actively viewed. Viewing a different agent or a non-Terminal screen does not suppress it.
- Scheduled actions run in non-delegated sessions and never enter the generic agent-lifecycle alert path. The first successfully persisted Calendar terminal result makes one separate best-effort push attempt for either completion or failure, deep-linking to the frozen Brain thread.
- Explicit future Calendar reminders remain locally scheduled after sync. They are a separate user commitment, not a runtime lifecycle producer.

Runtime delivery is intentionally at-most-once attempt, not reliable delivery. Missing registration, Expo/HTTP failure, a transient in-process Calendar event drop, or a daemon crash after the terminal commit can produce no OS alert. Zen does not retain an outbox or retry; the durable Calendar result and Brain projection remain available when the user next opens the app.

## Future Work

If notification behavior needs to expand later, evaluate it from observed product needs. Plausible independent additions are:

1. multi-device push registrations
2. richer classifier reasons for better summaries
3. optional per-run `notify_on_completion`

Those are later improvements, not prerequisites for shipping the OSS core.
