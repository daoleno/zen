# Mobile interface structure

This page describes how the Android and iOS app is laid out and which shared patterns each screen uses. Both platforms share every screen. On iOS 26 and later, the glass material uses system Liquid Glass. Elsewhere it falls back to a translucent fill with a hairline and a lit top edge. Reduce Transparency switches it to an opaque fill.

The guiding rule: a primary page shows only what you need right now. Low-frequency and advanced actions live in the page action menu, a sheet, or a detail page. Status marks appear only for states you can act on.

## Primary shell

- The app bar floats over content. It has a circular menu button, a segmented **Brain / Sessions** switch, and one page action button.
- The menu button stays clean while the current server is healthy. A dot appears only when there is something to act on: red for a connection issue, amber while the server is offline. The button's accessibility label names the state. Connecting is transient and shows no dot.
- The drawer is pure navigation:
  - A read-only header shows the current server and its connection state. The state text is colored only when the server is offline or has an issue.
  - One grouped card holds **Skills**, **Stats**, and **Settings**. Each destination appears once.
  - The footer shows the app version.

## Brain (launch screen)

Brain is the chat with the current server's Brain. Errors and historical read-only threads appear as inline notices above the chat. When the executor has no structured chat view, the empty state offers **Switch executor** and **Open terminal**. The page action opens the Brain action sheet: New chat, Switch executor, Open terminal, Browse workspace, and Calendar.

## Sessions

The Sessions page is the list of the current server's Sessions, grouped by directory into inset rounded cards. Above the list, notices appear only when needed:

1. **Server offline** (or the connection issue) with **Retry**, when the current server is unreachable.
2. **N Work items need you** with **Review**, when Work on the current server is blocked on you. Review opens the Work activity sheet.

In every other case nothing sits above the list.

- The floating **+** button creates a Session.
- The page action opens a menu with **New session**, **Work activity** (which shows how many items need you), and **Services**.
- Long press enters multi-select for termination.
- There is no pull-down gesture.

All data comes from the current server, and sheets close when you switch servers.

### Services

Services is a sheet with one row per listening port, grouped by project. Each row shows the port, the process, and its Session or persistent unit. A **Public** pill marks a running Quick Tunnel. Tapping a row opens its detail page inside the same sheet:

- **Open**: the service's URLs (or DSH Web), plus **Open Session terminal** when the Session is live.
- **Public access**: start a Quick Tunnel (after a confirmation alert), then open or copy the public URL, or stop the tunnel.
- **Details**: the process, command, bind address, and persistent status.

## Session screen

The header is three glass capsules: Back, the Session identity, and actions. The identity capsule shows a status dot for running, blocked, or failed Sessions, and tapping it opens Session details. Session actions open as a bottom sheet with **Terminate** last. Empty, loading, error, and "chat view unavailable" states use one chat-canvas empty state. A fresh Session shows **Ready** along with its workspace path.

### Chat

- The composer is one glass capsule: a quiet tinted **+** on the leading side and a filled accent send disc on the trailing side. Both are 34pt discs inside 44pt targets. The send disc is muted when disabled, shows dots while sending, and turns into a neutral stop disc (with elapsed time) while the agent runs.
- Attachments are uniform rounded tiles with an in-bounds remove target. Uploads show progress and a Cancel.
- User messages are rounded bubbles. Assistant replies are full-width prose. Tool and activity rows are quieter notes: a small tinted icon tile, muted 12pt title, and a detail rail when expanded.
- Composer errors appear as an alert strip above the capsule. The date divider is a pill, and jump-to-latest is a glass disc.

### Git

The Git sheet has one Back/Close control, a title with a one-line change summary, and a single **More actions** menu. Changed files sit in one inset group with status letter tiles. Diffs use soft add/remove tints with a two-column line gutter. See [git-diff-design.md](git-diff-design.md).

## Settings

Settings is one grouped list:

- **Servers**: each server shows a dot only when it is not connected. The current server carries an **In use** tag. Tapping a server opens its actions: Use, Connect, Disconnect or Retry, then Edit, then Remove.
- **Channels**: one **Telegram** row with the bot name and a status pill. It opens the Telegram page, which shows the identity, one primary next step (Verify token, Connect Telegram, Open Telegram, or Reconnect), grouped secondary actions, and diagnostics. Destructive actions (Unlink account, Remove bot) sit behind **Advanced**.
- **Providers**: opens Providers.
- **Appearance**: one segmented control (Auto / Light / Dark). Theme lives only here.

## Providers

A segmented **Codex / Claude** switch picks the agent. Below it, one grouped list shows which connection that agent uses: **Official login** first, then saved Providers. Each has a radio mark, its host, model count and catalog age, and any test result. A **Key required** pill marks Providers without a credential. Each Provider's **…** opens an action menu: Models, Test connection, Edit, and Delete (confirmed). **Add** sits in the section header. Search appears only once the list is long. The Zen Provider Gateway is its own row with a status pill, and tapping it copies the endpoint.

## Shared patterns

- **Overflow menus** always use the bottom-sheet `ActionMenu`. This covers Brain, Sessions, Session, Work detail, per-server actions in Settings, and per-Provider actions.
- **Destructive actions** go through `confirmDestructive` or a native alert where the destructive button is never the default. This covers terminating Sessions, removing a server, deleting a Work item, deleting a Provider, and removing or unlinking Telegram.
- **Status** uses `StatusPill`, whose live pulse stops under Reduce Motion. Recoverable problems and in-flow status use `InlineNotice`, and empty, loading, and blocking error states use `EmptyState`.
- **Lists** use grouped `ListSection` / `ListRow` rows. Small exclusive choices use `SegmentedControl`.
