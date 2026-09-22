# DSH Sessions

DSH is an additive New Chat and executor choice. Codex host and delegated defaults
remain unchanged. Zen launches the installed `dsh` web profile on loopback behind
an exact Session bridge; it does not embed Cordis or the DSH web application in
React Native. The installed headless CLI cannot resume interactive sessions; ACP
is text-only/fresh-session-only and SDK JSON-RPC lacks cancellation. Those narrower
interfaces are not advertised as the mobile Session implementation.

The bridge owns one native session ID, an exclusive lock and a private Unix socket.
Native session logs live under the daemon state's `provider-sessions/dsh/logs`.
DSH retains ownership of its saved provider, model, reasoning and permission choices.
Zen's Session Model control reads the native catalog for the selected route and
calls native `session.selectModel`; it does not rewrite `executors.toml`, DSH
settings or credentials. Selection acknowledgements report native asynchronous
persistence honestly. Existing Sessions are not migrated.

The conversation reader consumes native version-0 Zstandard JSONL and packed delta
rows. Streaming text and committed messages share a stable turn/step identity;
provider call IDs associate results with their tool cards. Plugin context stays out
of human chat. Native turn boundaries own Running, completion and interruption.
Persisted logs support history reload/reconnect without a phone URI. Native image
references resolve through their exact live Session and the existing generation
identity check. The common image viewer handles those results and ordinary file
references. Uploads become native images only from the authorized Zen upload root;
DSH's native 5 MiB image-intake limit applies. Other files remain explicit attachments.

Stopping sends native `session.cancel`; closing the bridge disposes its native
runtime. Linux parent-death signals also stop that child after an abrupt bridge
exit. The private Session socket disappears on orderly shutdown. Resume uses the
same owned session ID and native log rather than creating a second conversation.

Native configuration must match the installed DSH version. Startup failures are
reported in the bridge's private Session log; Zen never repairs credentials or
chooses a different provider automatically. DSH permission and question requests
appear in Interface's review sheet. Native server-request RPC IDs, Session
generation, and connection epochs fence every answer. Allow once, Reject,
structured option/custom answers, and cancellation
use native `/api/respond`; resolved, replaced, disconnected, or foreign-session
requests cannot be answered from stale UI. Native permissions remain fail closed.
Native terminal output is a minimal input bridge; the shared Interface provides
message, reasoning, tool and image presentation. Windows daemon hosts do not expose
the Unix Session bridge. Android and iOS use the same mobile implementation.
