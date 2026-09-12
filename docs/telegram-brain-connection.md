# Telegram Conversation Connection

Reviewed: 2026-09-12

Telegram is another UI for the current server's durable Brain, Sessions and Work.
It owns only bot credentials, the verified private-chat binding, Telegram destinations,
receipts, and delivery checkpoints. It never creates a separate agent registry.

## Connection

Open **Settings > Channels > Telegram** on Android or iOS.

- An unconfigured connection has one masked bot-token field and Verify token.
  BotFather is available for obtaining a bot, not a required topic-mode setup ritual.
- After verification, Connect Telegram opens a short-lived owner-binding link.
  Zen trusts the numeric sender and private chat, not a typed username.
- A bound connection shows its account, status and recipient. Disconnect pauses
  delivery without removing its binding; Reconnect resumes future output.
- Advanced contains mode, topic/conversation identifiers, unresolved delivery
  count, refresh, token replacement, unlink account and remove bot.
- Token input is cleared on submission, exit and backgrounding. The editor is
  unavailable while the current server is offline. No terminal is needed for
  supported configuration.
- Bot conversations are Telegram cloud chats. Removing Zen's configuration does
  not delete Telegram cloud history.

The app retains exactly one current server. Responses are request-correlated to
that server; old-server state and in-flight token work cannot migrate to another one.

Local operators may also use `zen telegram setup`, `status`, `enable` and
`disable`. These use the running daemon's existing private control socket.
They never start a second bot manager or read a credential from a CLI argument.

## Brain Continuity

Ordinary Brain input always enters `SubmitExternalUserInput` using the canonical
Brain host and thread. Only explicit **New Chat** or `/new` calls Brain's
`NewChat`. Binding, `/start`, native topic creation, reconnection and selection
do not create canonical conversations.

Telegram has two different private-topic flags:

- `has_topics_enabled` permits private bot topics.
- `allows_users_to_create_topics` changes the graphical client's General
  composer behavior. When true, clients replace General with "View as messages"
  and create a native topic before sending from that composer.

This distinction is documented by Telegram's [bot forums specification](https://core.telegram.org/api/forum#bot-forums).
A native Telegram topic is not a Brain thread. Zen provisions a Brain topic
once, remembers its exact ID, and places Brain navigation there. An observed
owner-created topic may also route to the same current Brain conversation.
Replies remain in that observed native topic; Back to Brain selects the primary
Brain topic. Unknown topics without a creation observation fail closed.

When private topics are not enabled, ordinary private-chat text continues Brain.
`/sessions` supplies native inline Session choices and a numbered list;
`/use <number|exact-session-id>` selects the recipient, and `/brain` returns
to Brain. Numbers resolve against the last persisted chooser, never a newly
reordered inventory. A missing selected Session remains unavailable until an
explicit switch, so a later message cannot silently fall through to Brain.

## Session Conversations

Session topics are keyed by exact current delegated Session ID and private chat
ID. Names are display-only. Hidden Brain hosts and manual/nondelegated Sessions
do not enter the Telegram inventory.

- `/sessions` lists the current inventory. Inline choices either open the exact
  existing Session topic with its status or select its private-chat fallback.
- Native topic messages route only to the mapped Session through
  `SubmitExternalSessionInput` and the watcher's generic receipt-owned input
  queue. There are no Telegram-specific provider commands or byte checks.
- Replies to recorded Session output retain the exact Session destination even
  when the private-chat recipient has since changed.
- `/status` reports Session/Turn/Work state. `/brain` and `/sessions`
  navigate without starting work. `/new` in a Session topic does not reset Brain.
- Assistant output is projected after the mapping/reconnect boundary, using
  stable event/chunk IDs. Partial output edits the existing message.
- Completion markers follow canonical Turn and Work facts. A current active
  Turn takes precedence over an older terminal Work when presenting activity.
  Direct follow-up never manufactures a Work completion or acceptance.
- Unavailable, unmapped or stale destinations do not send to Brain, create a
  replacement worker, or infer success.

## Non-Destructive Reconciliation

A Session missing from one watcher snapshot is not proof of death.
`SessionProjection.AbsenceConfirmed` uses the watcher's strict
`ResolveDelegatedAbsence` check. Only confirmed absence retires a topic.

Retirement retains the exact Session/topic tombstone and delivery checkpoints,
cancels pending projected updates, and renames the Zen-owned topic **Closed**.
It never deletes Telegram history or kills local tasks. Pending legacy
delete/close/reopen operations are cancelled by the private-chat adapter.
A still-live exact Session may become active again; it is never replaced by
a name match or by a new ID.

The Bot API documents `closeForumTopic` and `reopenForumTopic` for forum
supergroups, not private chats. `deleteForumTopic` erases topic history.
A Closed label and tombstone are therefore the private-chat counterpart.
The Bot API has no topic enumeration method. A send failure is not enough to
prove deletion or authorize automatic recreation; failures remain explicit.

## Durable Delivery

The private `telegram/state.json` schema is 4. Schema 1/2/3 migrations preserve
the owner, bot, cursor, outbox and successful topic identities. Optional schema-4
fields contain Brain destinations, callback routes/IDs, chooser order and reply
destinations. No token is present in this file; `telegram/token` is a separate
0600 file. State replacement is atomic and failed persistence cannot become
in-memory routing authority.

Inbound receipts use `telegram:update:<bot-id>:<update-id>`. Brain admissions
are durable in the real Brain Store. The Telegram cursor advances only after
the outcome is recorded. Callback IDs are separately deduplicated in a bounded
journal; duplicates are still answered to clear Telegram's spinner.
`allowed_updates` includes both messages and callback queries.

Outbound rows persist pending, then dispatching, then sent/failed/ambiguous.
Definite flood waits honor `retry_after` across the private chat, including other
rows and topic operations. Transport-indeterminate outcomes are retained for
inspection and are not blindly replayed. Reconnection preserves those outcomes.

Reconnect starts a new delivery boundary and removes only pending canonical
projections. Sent history, receipt checkpoints, direct replies and ambiguous
outcomes remain. Output accumulated while disabled is not backfilled.
A short receive wait bounds reply latency; bot/webhook capabilities refresh on
a slower cadence. All outbound mutations use the same serialization owner.

Markdown uses Goldmark and explicit Telegram entities, not ad hoc MarkdownV2.
Messages are chunked at 4096 UTF-16 units without splitting astral characters.
Local-file and relative Markdown links remain readable text with their paths;
they are not sent as invalid Telegram HTTP link entities. A definitely rejected
invalid-link row from the current delivery interval may be re-rendered once under
the same identity after correction; ambiguous rows are never included in this repair.
Formatting rejection permits the same row's plain fallback; ambiguous failure
never triggers another format/send. Raw tool protocol is not forwarded.

## Security And Limits

Only a concrete non-bot sender in the verified private chat may submit input.
Groups, sender-chat identities, foreign owners and edited messages cannot gain
authority. Inline labels and callback data never grant new ownership.

Bot API errors are redacted; no tokens, pairing links, prompts or unrelated
chat bodies belong in logs. The connection checks `getWebhookInfo` and refuses
to poll when a webhook exists. It never steals a webhook or enables paid broadcasts.
One joined daemon runtime owns the bot.

Media transfer remains outside this adapter; unsupported media produces a
concise response without provider input. iOS and Android share the Settings
contract. Native iOS runtime proof still requires an actual supported simulator
or device; an Expo export is only a bundle check.

## Official References

- [Bot forums and General behavior](https://core.telegram.org/api/forum#bot-forums)
- [Bot API](https://core.telegram.org/bots/api), especially getMe, Message,
  CallbackQuery, sendMessage, createForumTopic, editForumTopic and closeForumTopic
- [Bot FAQ and flood control](https://core.telegram.org/bots/faq)
- [Telegram Privacy Policy](https://telegram.org/privacy)
