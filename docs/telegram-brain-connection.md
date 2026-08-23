# Telegram Brain Connection

Date reviewed: 2026-08-24

## Product Boundary

Telegram is an outbound-network, daemon-owned adapter for the canonical Zen Brain. It is not a
provider transcript, executor, scheduler, relay, or chat database. Brain owns the current thread,
input admission, timeline, Work, Events, Sessions, and typed resolutions. Telegram owns only its
credential, owner binding, update cursor/dedupe, delivery attempts, and Telegram message mappings.

The first slice supports one bot, one immutable numeric Telegram owner, and that owner's one private
bot chat. It uses Bot API long polling and therefore requires no inbound port, webhook, hosted
service, tunnel, or separate Node process.

## Official Capability Evidence

Sources were read on 2026-08-24. Telegram's official documentation is authoritative:

- Bot API 10.2: <https://core.telegram.org/bots/api> (current release dated 2026-07-14).
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

Bot API 10.2 does support user-visible topics in a private chat with a bot when BotFather forum
topic mode is enabled: `getMe` returns `has_topics_enabled` and
`allows_users_to_create_topics`; `Message.message_thread_id` applies to supergroups and private
chats, and send methods accept it for private chats of bots with topic mode enabled. This is distinct
from `DirectMessagesTopic`, which belongs to a channel's direct-messages chat, and from Telegram
Business connections, which let a bot manage explicitly granted private chats on a business
account. Zen does not enable or imitate any of these modes in the first slice. A topic-to-Brain
mapping needs a canonical Zen owner before it can be added safely.

Text sent with `sendMessage` is limited to 1-4096 characters after entity parsing. The first slice
chunks plain text on paragraph/line/rune boundaries, sends without a parse mode, and therefore
cannot mis-escape Markdown. `editMessageText` supports the same 4096-character limit and is used to
preserve one Telegram message per canonical Work card. Reply context is available as
`reply_to_message` (without recursively nested replies); text and media captions are accepted as
bounded context. The initial slice sends no attachment bytes: unsupported media receives one
actionable response and creates no Brain admission.

`sendMessageDraft` now provides an ephemeral 30-second partial preview which must be followed by a
complete `sendMessage`. Typing actions, drafts, rich messages, reactions, inline keyboards,
callback queries, and aggressive streaming edits are deferred: none improves first-slice authority
or durability, and callbacks add another identity/action surface. If callbacks are later added,
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
rows, canonical-item checkpoints, and Work-to-message mappings. Both files are daemon-local and are
independent of saved mobile server records; removing a mobile server does not delete the binding.
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
3. A definite Bot API rejection may return to `pending` for bounded retry (including
   `retry_after`). A successful response persists `sent` and its Telegram message ID.
4. A network/decoding interruption after dispatch is `ambiguous` and is never automatically
   replayed. On restart, any residual `dispatching` row becomes `ambiguous` before polling.
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

## Deferred Capabilities

Private-chat topics, channel direct-message topics, Business bots, callbacks/buttons, reactions,
draft streaming, rich messages, and file transfer are later channel enhancements. They require
canonical product mappings, additional identity checks, or bounded attachment plumbing and are not
needed to prove the secure owner-bound text path.
