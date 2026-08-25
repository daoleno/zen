# Telegram Brain Connection

Date reviewed: 2026-08-25

## Product Boundary

Telegram is an outbound-network, daemon-owned adapter for the canonical Zen Brain. It is not a
provider transcript, executor, scheduler, relay, or chat database. Brain owns the current thread,
input admission, timeline, Work, Events, Sessions, and typed resolutions. Telegram owns only its
credential, owner binding, update cursor/dedupe, delivery attempts, and Telegram message mappings.

The first slice supports one bot, one immutable numeric Telegram owner, and that owner's one private
bot chat. It uses Bot API long polling and therefore requires no inbound port, webhook, hosted
service, tunnel, or separate Node process.

## Official Capability Evidence

Sources were reviewed on 2026-08-25. Telegram's official documentation is authoritative:

- Bot API 10.3: <https://core.telegram.org/bots/api>.
- Bot features: <https://core.telegram.org/bots/features>.
- Bot FAQ: <https://core.telegram.org/bots/faq>.
- Telegram Privacy Policy, sections 3.3 and 6: <https://telegram.org/privacy>.

`getUpdates` and webhooks are mutually exclusive. Updates remain at Telegram for at most 24 hours.
An update is acknowledged when `getUpdates` is called with `offset > update_id`; offsets must be
recalculated after every response to avoid duplicates. The API accepts 1-100 updates, a positive
long-poll timeout, and `allowed_updates`; changing the filter does not affect already-created
updates. The first slice requests only `message`. It calls `getWebhookInfo` before polling and
reports a non-empty webhook URL as a conflict. It never calls `deleteWebhook` implicitly.

Webhook delivery uses HTTPS POST and retries non-2xx responses for a finite, unspecified reasonable
number of attempts. `setWebhook.secret_token` is 1-256 characters from `[A-Za-z0-9_-]` and arrives
in `X-Telegram-Bot-Api-Secret-Token`; supported ports are 443, 80, 88, and 8443. Webhooks are not in
scope because long polling is the required outbound-only topology.

Bot API 10.3 supports user-visible topics in a private chat with a bot when BotFather forum
topic mode is enabled: `getMe` returns `has_topics_enabled` and
`allows_users_to_create_topics`; `Message.message_thread_id` applies to supergroups and private
chats, and send methods accept it for private chats of bots with topic mode enabled. This is distinct
from `DirectMessagesTopic`, which belongs to a channel's direct-messages chat, and from Telegram
Business connections, which let a bot manage explicitly granted private chats on a business
account. Zen now implements private-chat Topics as a first-class routing surface with durable
Session mappings; the authoritative facts and the exact mapping/lifecycle/recovery model are
documented in the "Session Topics" section below.

Text sent with `sendMessage` is limited to 1-4096 UTF-16 code units after entity parsing.
`MessageEntity.offset` and `MessageEntity.length` are measured in UTF-16 code units. Zen parses
canonical assistant Markdown with Goldmark and sends plain display text plus explicit `entities`;
it does not select a parse mode or blindly enable MarkdownV2. Supported presentation includes
headings, bold, italic, strikethrough, links, inline code, fenced and indented code, blockquotes,
lists, task lists, tables, line breaks, and readable visible raw HTML. Unsupported or malformed
constructs remain readable plain text. Chunks are at most 4096 UTF-16 code units, never divide an
astral rune, prefer paragraph/line/word boundaries, and clip or repeat overlapping entity ranges
per chunk. `editMessageText` supports the same limit and entity contract and is used to preserve
one Telegram message per canonical Work card. Reply context is available as
`reply_to_message` (without recursively nested replies); text and media captions are accepted as
bounded context. The initial slice sends no attachment bytes: unsupported media receives one
actionable response and creates no Brain admission.

`sendChatAction` with `action=typing` displays native typing status for up to five seconds or until
a message arrives. Zen sends it immediately after exact accepted Brain input, renews every four
seconds only while the complete durable `HostForegroundTurn` identity remains current, and stops
on exact turn closure/replacement, disable/revoke/remove, parent cancellation, or a ten-minute
safety deadline. Chat actions are best-effort, process-local, and create no Work, Event, timeline,
or outbox row. `sendMessageDraft` provides an ephemeral partial preview which must be followed by a
complete `sendMessage`; drafts, reactions, inline keyboards, callback queries, and aggressive
streaming edits remain deferred. If callbacks are later added,
`CallbackQuery.from.id` must be authorized and every query answered to clear Telegram's client-side
progress indicator.

`getFile` permits Bot API downloads up to 20 MB and returns a token-bearing URL valid for at least
one hour. Bot API uploads are up to 10 MB for photos and 50 MB for other multipart files (URL sends:
5 MB photos, 20 MB other files). Telegram file support is deferred until bytes can enter Zen's
existing bounded upload/attachment contract without exposing token URLs or host paths.

Flood control returns `ResponseParameters.retry_after`. Telegram's FAQ says to avoid more than one
message per second in one chat, 20 per minute in a group, and about 30 messages per second for free
broadcasts. Zen serializes one-owner delivery, respects `retry_after`, and uses bounded jittered
backoff. Paid broadcasts are never enabled.

Bot commands are up to 32 Latin letters, digits, or underscores. Telegram recommends global
`/start` and `/help`; command scopes are presentation only and do not authorize received commands.
Zen supports `/start <challenge>`, `/help`, `/status`, and `/new`. `/new` invokes Brain's canonical
new-chat operation and never creates Telegram-local thread state.

Deep-link `start` payloads allow `A-Z`, `a-z`, `0-9`, `_`, and `-`, up to 64 characters. Zen uses a
random base64url challenge, with expiry and single-use state persisted by the trusted daemon. The
message's numeric `from.id` and private numeric `chat.id` become authority; usernames and names are
display hints only. Telegram user/chat identifiers can exceed 32 bits but fit in signed 64-bit
storage for Bot API values.

Bot private chats are Telegram cloud chats, not Secret Chats. Telegram stores cloud messages,
photos, videos, and documents on its servers. Telegram's privacy policy also states that bot
developers receive messages and public account data when a user interacts with a bot. Zen must
therefore describe Telegram as an external retention boundary and must never imply end-to-end
encryption to the daemon.

## Architecture And State

The adapter runs as a joined daemon runtime owner. Context cancellation stops an in-flight long
poll and all backoff waits. The HTTP client has explicit request/header/idle timeouts and a bounded
response body. The token is read from `<daemon-state>/telegram/token` with mode `0600`; public
status, JSON state, logs, errors, metrics, CLI arguments, and mobile payloads never contain it.

`<daemon-state>/telegram/state.json` is atomically replaced with mode `0600`. It contains public bot
identity, binding/challenge metadata, the next update offset, durable update dispositions, outbox
rows, canonical-item checkpoints, Work-to-message mappings, and the durable owner-binding delivery
boundary. State schema 2 explicitly migrates schema-1 rows as plain deliveries. Each new logical
formatted row persists its display text, plain fallback, entity array, and current formatted/plain
variant. Brain timeline rows at or before that boundary remain in Brain and are never backfilled to
Telegram; only rows created after the connection starts can enter the Telegram outbox. Both files
are daemon-local and are independent of saved mobile server records; removing a mobile server does
not delete the binding.
Processed-update history retains only a bounded window behind the durable monotonic offset. The
outbox has a hard row bound, compacts only terminal sent/failed history, and never compacts pending,
dispatching, or ambiguous rows to make room; saturation is a visible degraded state.

Inbound state transition:

1. Receive update N without acknowledging it.
2. Reject edited/non-message updates by subscription, and reject every non-private chat, absent or
   bot sender, sender-chat/anonymous identity, non-owner identity, or wrong bound chat.
3. For binding, atomically consume an unexpired exact challenge and persist numeric owner/chat.
4. For owner text, create stable receipt `telegram:update:<bot-id>:N`, call Brain's canonical
   prepare/provider-mutation/admit path, and preserve its accepted/not-submitted/uncertain result.
5. Persist update disposition and `next_offset=N+1` together. Only then may the next `getUpdates`
   acknowledge N. On restart, the durable disposition and Brain receipt suppress duplicate
   admission.

Outbound state transition:

1. Derive deterministic logical rows from the current canonical Brain timeline only.
2. Persist `pending`, then persist `dispatching` before calling Telegram.
3. A retryable definite Bot API response may return to `pending` for bounded retry, preserving an
   explicit `retry_after`. Only a definite HTTP 400 entity/format rejection may change a formatted
   row back to plain `pending`; it retries once through the same row ID. A successful response
   persists `sent` and its Telegram message ID.
4. Transport failure, timeout, body-read failure, malformed envelope, result-decode failure, or a
   crash after `dispatching` becomes `ambiguous`; none can activate plain fallback or replay. On
   restart, any residual `dispatching` row becomes `ambiguous` before polling.
5. Assistant timeline IDs produce one logical send. Canonical Work IDs own one Telegram status
   message; later card revisions use `editMessageText`. A missing/uneditable mapped message is
   surfaced as degraded rather than silently creating event spam.

This is deliberately stronger than at-least-once delivery. Telegram has no caller-supplied
idempotency key for `sendMessage`, so a crash after Telegram commits but before Zen records the
message ID cannot be reconciled without reading chat history. Automatic replay would duplicate
content; the fail-closed outcome is visible ambiguity.

## Threat Model

| Threat | Boundary and mitigation |
| --- | --- |
| Stolen bot token | Attacker can impersonate the bot and consume updates. Store separately at `0600`, redact all errors, support atomic rotation/removal, and report webhook/poll conflicts. Rotation is required after suspected exposure. |
| Leaked start challenge | Random, short-lived, single-use, private-chat-only challenge; consuming it atomically binds exact numeric sender and chat. Replays and stale values fail closed. |
| Telegram account takeover | Numeric identity still authenticates the compromised account; Zen cannot detect Telegram compromise. Revoke from a trusted Zen device and secure Telegram with its account controls. |
| Replayed/duplicate updates | Durable update disposition plus monotonic acknowledged offset and stable Brain receipt. Never derive authority from usernames or update order alone. |
| Group/channel addition or anonymous sender | Require `chat.type == private`, concrete non-bot `from`, no `sender_chat`, exact owner ID, and exact chat ID. Reject before parsing commands or content. |
| Forwarded identity/content | Authorization uses the actual `from`, never `forward_origin`. Forwarded text remains untrusted prompt content from the owner; it grants no identity. |
| Attachment prompt injection | First slice admits no media bytes. Captions are owner-authored input but remain untrusted prompt content. Future media must use Zen's bounded attachment contract and explicit type/size gates. |
| Daemon crash/restart | Atomic state, durable offset/disposition, canonical Brain receipt, and startup conversion of `dispatching` to no-replay `ambiguous`. Joined runtime cancellation proves clean shutdown. |
| Outbound ambiguity | Do not replay an indeterminate send/edit. Surface degraded state and retain the logical row for operator reconciliation. |
| Telegram/cloud retention | Bot DMs are cloud chats stored by Telegram and delivered to the daemon. UI/setup must state this boundary; deleting Zen state does not assert deletion from Telegram. |
| Logs and metrics | Never log token, bodies, numeric identifiers, challenge, or Telegram error descriptions that may echo request data. Use bounded counts and reason classes only. |
| Remote destructive action | Plain input enters Brain's normal judgment/admission path. Telegram has no privileged buttons in this slice and cannot bypass Zen approval or typed Work resolution. |

## Session Topics

### Official capability facts (reviewed 2026-08-25)

Sources: Bot API 10.3 (`core.telegram.org/bots/api`), Bot API changelog
(`core.telegram.org/bots/api-changelog`, Bot API 9.3 of 2025-12-31 and 9.4 of 2026-02-09), and the
core Telegram protocol page `core.telegram.org/api/forum`.

- Private-chat topics are the BotFather "Threaded mode" / forum topic mode for bots: `getMe`
  returns `has_topics_enabled` (topic mode enabled for the bot in private chats) and
  `allows_users_to_create_topics` (users may create/delete topics; controlled by the BotFather
  "Disallow users to create new threads" setting). The core protocol page states the feature is
  subject to an additional fee for Telegram Star purchases (ToS section 6.2.6).
- `createForumTopic` supports bot-created topics in private chats (Bot API 9.4). It has **no
  caller-supplied idempotency key**; the returned `ForumTopic.message_thread_id` is the only
  durable topic identity. `editForumTopic`, `deleteForumTopic`, and
  `unpinAllForumTopicMessages` also support private chats (Bot API 9.3).
- `closeForumTopic` and `reopenForumTopic` are documented for **forum supergroups only**; they were
  never added to the private-chat support list. Zen therefore never issues them for its private
  chat: the completion lifecycle is projected with marker messages ("clearly marks"), and the
  close/reopen op states exist only for the future supergroup variant and for direct unit
  coverage of the op state machine.
- The "General" topic is non-deletable with `id=1`; messages and updates in forum topics carry the
  topic id as their thread id **except for the General topic**, whose messages carry no thread id
  at all. Sending to General is a normal chat send without `message_thread_id`. Bot API mirrors
  this: `Message.message_thread_id` is absent for General messages and `sendMessage`
  `message_thread_id` targets a specific topic (`core.telegram.org/api/forum`; Bot API
  `sendMessage`/`sendChatAction` parameter docs).
- `Message.is_topic_message` is set for topic messages; `forum_topic_created`/`forum_topic_edited`
  service messages describe user-created topics. `editMessageText` has **no**
  `message_thread_id` parameter: edits address an existing message by message id and inherit its
  thread. `sendChatAction` accepts `message_thread_id` for topic-scoped typing.
- The Bot API has no method to enumerate a chat's topics, so Zen cannot list, reconcile, or verify
  a deleted/foreign topic; it records what it creates and fails closed otherwise.

### Routing authority

- The Brain conversation has one explicit Brain route: the General topic (`message_thread_id`
  absent or `1`). General input goes only through `SubmitExternalUserInput` (canonical Brain
  admission) and is never sent to a delegated Session.
- Each user-visible delegated Session (watcher agent with `Delegated == true`, not hidden, not the
  host) may own exactly one Telegram Topic. The durable mapping stores exact Brain thread
  (`Work.SourceThreadID`, or the current chat thread when no Work exists), Work ID when present,
  delegated Session ID, Telegram chat ID, and `message_thread_id`. Routing authority is the exact
  numeric Session ID + chat ID; topic labels are presentation only.
- Input in a mapped Session topic goes directly to that exact Session through
  `SubmitExternalSessionInput` (the watcher's receipt-owned provider mutation path, identical to
  mobile `send_input` for delegated sessions). It is never wrapped as a Brain message, never
  summarized by Brain, never routed to General, and is never busy-rejected: multiple owner
  messages sent while the Session works are accepted in order by the watcher's per-session
  serialization and the provider's native pending queue. An unknown/unmapped, stale, dead,
  ambiguous, or terminal-without-reopen Topic fails closed with a concise actionable reply in that
  same Topic.
- Commands are route-local. General keeps `/help`, `/start`, `/status`, `/new` unchanged. In a
  Session topic, `/status` reports that exact Session's lifecycle state and `/help` explains the
  topic contract; `/new` never executes there (it is the Brain new-chat operation) and replies
  with an actionable pointer to General.

### Durable topic state (schema 3)

- `topics`: per-mapping records — SessionID, ThreadID (Brain thread), WorkID, ChatID,
  MessageThreadID, Label, State (`active`, `completed`, `stale`), CreatedAt/UpdatedAt, and
  no-replay message checkpoints in `TopicProjection`/`TopicMessages`. Schema 2 -> 3 keeps every
  existing row intact; no topics existed in schema 2, so only the new maps and empty slices are
  added.
- `topic_ops`: durable topic operations (create/rename/close/reopen/delete) with the same
  pending -> dispatching -> sent/failed/ambiguous state discipline as the message outbox. On
  restart any residual `dispatching` op becomes `ambiguous`; ambiguous ops are never retried
  automatically.

### Create/lifecycle policy

- When the connection is bound, topic mode is available, and a new user-visible delegated Session
  appears without a mapping, Zen enqueues one `create` op for it (stable ID
  `topic:create:<sessionID>`). A definite Bot API rejection returns the op to `pending` with
  bounded backoff (safe: no topic was created). A transport-indeterminate outcome converts the op
  to durable `ambiguous`; the Session is never re-created automatically, no duplicate Topic is
  created to hide ambiguity, and the ambiguity is surfaced in connection Status as an actionable
  degraded state.
- Topic name = Session display label truncated to 128 characters. A label change enqueues a
  `rename` (edit) op with an ID bound to the new label; rename failures are terminal and
  opportunistic (labels are presentation only; the next label change retries under a new ID).
- Lifecycle projection into the Session's topic: assistant messages produced after mapping
  creation (sanitized projection; no tool protocol, no prompts, no envelopes), turn/work status
  markers on transition (working/completed/failed/Work done/cancelled), and a "Session is no
  longer available" marker when the Session disappears. General keeps its existing Brain
  assistant/work-card projection unchanged.
- Completion: a terminal canonical turn or terminal Work "clearly marks" the Topic with a
  lifecycle marker; history is never deleted. Reopen policy: while the Session is still viable
  (agent present), new direct input in that Topic is admitted and the mapping returns to `active`;
  a Session that is absent/dead becomes `stale` and fails closed. The exact same Session identity
  becoming user-visible again revives the stale mapping with a one-shot marker (same durable
  Session id, same surface); a genuinely new Session gets its own mapping.
- Deleting a Topic (user or future supergroup support) is never automatic; Session history stays.
  Definite send failures into a mapped Topic (e.g. "message thread not found") fail closed: the
  mapping is kept, the row fails terminally, and Status surfaces the degradation. A documented
  Bot API 10.0-side regression (`tdlib/telegram-bot-api#847`-style "message thread not found"
  for private topics) means such a failure is not proof of deletion, so no recreate is attempted.
- Disable stops polling/delivery/input without weakening credentials or the current mapping;
  Enable restores it. Revoke/remove/rotation clears topic state together with the rest of the
  bound channel state.

### Admission/reliability invariants

- Incoming updates keep the durable disposition + offset + restart dedupe. Mapped-topic input
  uses the stable receipt `telegram:update:<botID>:<updateID>` (same namespace as Brain input;
  one update routes to exactly one surface). At-most-once: a definite pre-mutation failure may be
  retried under the same durable identity; an unknown provider admission is no-replay and is
  surfaced as uncertain without a retry.
- Typing follows the Session topic: after accepted direct input, `sendChatAction(action=typing)`
  is sent with the topic's `message_thread_id` and renewed while the Session's canonical turn is
  non-terminal; it stops on terminal state, Session absence, disable/revoke, context cancel, or
  the safety deadline. It remains best-effort, process-local, and non-durable.

## Deferred Capabilities

Channel direct-message topics, Business bots, callbacks/buttons, reactions, draft streaming, and
file transfer are later channel enhancements. Group/supergroup Topic support (including
`closeForumTopic`/`reopenForumTopic` and topic enumeration) stays deferred: this adapter is the
one private Bot chat, and the Bot API has no topic-list method. A later Work-level grouping could
layer multiple Work IDs under the same Session routing surface without changing routing
authority: the Session ID remains the canonical owner and the durable mapping stays
Session -> topic; grouping by Work would only add a read-side aggregation and a per-Work marker
namespace.
