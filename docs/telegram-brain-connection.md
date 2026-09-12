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

- `/sessions` lists the current inventory in the topic where it was requested.
  Existing topic choices use URL buttons with Telegram's forum-topic link form
  `https://t.me/<bot>/<topic-id>?thread=<topic-id>`, not callback acknowledgements
  pretending to switch the client's view. Private-chat fallback still uses
  owner-validated callbacks to select the exact Session.
- Old topic-selection callbacks return the current Session/Turn/Work status and
  an Open Session link in the source topic. A topic not yet provisioned reports
  that state explicitly. Brain navigation offers the primary topic link;
  `/brain` acknowledges in the source topic, without retargeting that topic's
  Session identity. Clients without topic-link navigation can use Telegram's
  native topic list. No private `t.me/c/<user-id>` or callback game URL is used.
- Native topic messages route only to the mapped Session through
  `SubmitExternalSessionInput` and the watcher's generic receipt-owned input
  queue. There are no Telegram-specific provider commands or byte checks.
- Replies to recorded Session output retain the exact Session destination even
  when the private-chat recipient has since changed.
- `/status` reports Session/Turn/Work state. `/brain` and `/sessions`
  navigate without starting work. `/new` in a Session topic does not reset Brain.
  Session keyboards omit New Chat; old Session New Chat callbacks are rejected.
  A missing private-chat recipient stays visibly unavailable in `/status` and
  Settings until an explicit selection. Failed topic operations degrade the
  connection status rather than appearing healthy.
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
It also explicitly requests `message_reaction`. Telegram documents administrator
status as a prerequisite for those updates, so native owner-reaction delivery is
not promised in private bot chats. Inline feedback is the supported counterpart.

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
Messages are chunked at 4096 UTF-16 units using Unicode grapheme boundaries, so
emoji variation selectors, skin tones, flags and ZWJ sequences stay together.
An exceptional single grapheme longer than the entire Telegram message limit is
left intact for explicit rejection, not split or silently corrupted.
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

## Files And Captions

Photos and documents enter the same current Brain or exact selected Session as
text. Native topic mapping takes precedence. In private-chat fallback, replying
to a recorded Session output preserves that exact Session. A missing recipient
never falls through to Brain. A batch captures its recipient before downloading;
changing the selected Session does not retarget it. Changing the current Brain
conversation rejects an older staged batch, including at canonical admission.

The largest photo variant is selected. Safe original document names, content
types, byte sizes, captions and their UTF-16/custom-emoji entities are preserved.
Files use the existing `zen_attachments` envelope with local `name`/`path`
references, extended with size/type/description and caption metadata. This is
the same provider input mechanism as mobile uploads, not a Telegram-specific
prompt, native-vision request, or separate conversation. Actual image/file
interpretation depends on the selected executor's tools and capabilities.

The authenticated mobile upload handler and Telegram share one upload store and
reservation owner: random local names, 0600 files, 8 GiB aggregate capacity and
seven-day retention. Telegram adds a 20 MiB per-file and total-batch limit, a
30-second download-attempt deadline and at most three attempts. `getFile` is
refreshed on retry. Tokens/download URLs are never stored in attachment metadata
or returned in errors. Only the fixed API origin is contacted; redirects,
traversal, unsafe display names, invalid entity offsets and oversized bodies
are rejected. The adapter never executes files or extracts archives.

One cancellable download worker belongs to the existing Telegram manager. It
performs file IO and durable checkpoints only, without holding the outbound
send mutex during HTTP requests. Polling, text input, callbacks and output keep
running while a file is pending. The polling owner admits at most one completed
media batch per pass and rechecks the captured recipient before provider input.
Media batches retain FIFO order, including album collection and retry waits;
ordinary text and navigation do not wait on that queue. Disable, token/bot
rotation, owner revocation and shutdown cancel active IO. Shutdown joins the
worker; a replacement download cannot overlap an older cancelled worker.

Completed-file rename and reservation release share one upload-store critical
section, so concurrent mobile/channel uploads do not temporarily count both
committed bytes and the same reservation against capacity.

Albums are durably collected for two quiet seconds, capped at ten seconds from
the first item, ten files and 20 MiB total. Telegram supplies no album-end event.
The sealed batch produces one provider input containing every received caption;
late items get explicit resend feedback and do not start another turn. Partial
download success survives restart without redownloading already stored bytes.
No caption-only turn is submitted when a file fails. Terminal batch errors ask
for the whole batch to be resent. Receipt metadata is bounded to 128 batches and
expires after 24 hours; file retention is owned by the shared store.

Static stickers carry the actual WebP file plus their emoji descriptor. Animated
and video stickers carry a file reference and an explicit "animation not
interpreted" descriptor. Audio, voice, video, animation and video notes remain
file references with honest no-transcription/no-motion-interpretation labels.
There is no transcoder, transcription service or extra paid model call.

After a durable accepted admission, Zen replies "Files received by Zen" in the
source topic. This acknowledges input receipt, not Work completion. Uncertain
provider outcomes are not replayed. A crash at the admission boundary likewise
produces uncertainty rather than a duplicate turn.

The canonical channel output projection currently supplies text, not an
attachment-output contract. Generated local file links remain readable paths;
automatic Telegram `sendDocument`/`sendPhoto` replies are not advertised.

## Reactions

Ordinary reactions are message-scoped feedback only. Owner validation, exact
message-to-Session/Brain attribution, update deduplication and reaction removal
apply without calling a provider, creating a conversation or issuing commands.
The bounded journal retains sources for the latest 512 attributable messages;
unavailable or older messages fail closed.

Assistant replies offer thumbs-up, thumbs-down and Clear feedback buttons.
These persist feedback on that exact message and acknowledge via a callback
toast. They are not a claim that Telegram delivered a native reaction update.
The native `message_reaction` handler is defensive and uses the same journal;
private-chat inbound delivery remains unverified and administrator-gated in the
official Bot API documentation.

`setMessageReaction` can set/change/remove a bot reaction in the current private
topic mode (verified with an owned QA message). This does not imply that owner
reaction updates are available. Automatic input/status reactions are not used;
ordinary concise receipt feedback and canonical Work/Turn status remain in use.

## Stable Brain Entry

Zen reuses the persisted primary Brain topic. It creates one navigation message
with the exact primary-topic link and pins that **message** using
`pinChatMessage`. The send/pin operations use the existing durable, serialized
outbox. Restart does not create another entry. A failed or ambiguous pin does
not imply success or trigger a replacement topic. Assistant messages also
retain a Brain navigation button.

Telegram's **All / View as messages / General** navigation is client-owned.
The Bot API cannot replace, hide or reorder All, and has no topic-pin method.
MTProto's `messages.updatePinnedForumTopic` and
`messages.reorderPinnedForumTopics` are distinct client API methods, not Bot API
capabilities. Pinning a message does not pin its topic in the left topic list.

To keep Brain high in the topic list, open the bot's topic list, long-press
**Brain** on mobile (right-click on Desktop), and choose **Pin** when that
client exposes it. All remains client-owned. No Threaded-mode/BotFather change,
new bot, group conversion, renamed substitute for All or topic deletion is used.

Android and iOS share the channel behavior and Settings contract. Actual
signed-in client UI proof requires a user-owned Telegram client session; an
API receipt or bundle export is not a client screenshot.

## Official References

- [Bot forums and General behavior](https://core.telegram.org/api/forum#bot-forums)
- [Forum topic links](https://core.telegram.org/api/links#forum-topic-links)
- [Inline URL buttons](https://core.telegram.org/bots/api#inlinekeyboardbutton)
- [Bot API](https://core.telegram.org/bots/api), especially getMe, Message,
  CallbackQuery, getFile, Sticker, MessageReactionUpdated, Update,
  setMessageReaction, pinChatMessage, sendMessage, createForumTopic,
  editForumTopic and closeForumTopic
- [Bot FAQ and flood control](https://core.telegram.org/bots/faq)
- [Telegram Privacy Policy](https://telegram.org/privacy)
