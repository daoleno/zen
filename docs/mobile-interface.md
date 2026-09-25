# Mobile interface structure

This page describes how the Android and iOS app is laid out and which shared patterns each screen uses. Both platforms share every screen. On iOS 26 and later, the glass material uses system Liquid Glass. Elsewhere it falls back to a translucent fill with a hairline and a lit top edge. Reduce Transparency switches it to an opaque fill.

## Primary shell

- The app bar floats over content. It has a circular menu button, a segmented **Brain / Sessions** switch, and one page action button.
- The menu button carries a status dot for the current server: green when connected, amber while connecting, red when there is a connection issue, and grey when offline. With no server there is no dot.
- The drawer shows a glass card for the current server, with its name and connection status. Tapping the card opens Settings. Below it are **Skills** and **Stats** in one grouped card, and **Settings** sits in the footer.

## Brain (launch screen)

Brain is the chat with the current server's Brain. Errors and historical read-only threads appear as inline notices above the chat. When the executor has no structured chat view, the empty state offers **Switch executor** and **Open terminal**. The page action opens the Brain action sheet: New chat, Switch executor, Open terminal, Browse workspace, and Calendar.

## Sessions (overview)

The Sessions page opens with an overview of the current server:

1. The server's connection status and a count summary (sessions, running, need you).
2. An offline or issue notice with **Retry** when the server is not connected.
3. Quick actions: **New session**, **Services**, and **Work**. The Work button shows a badge when Work needs you.
4. **Work in progress**: up to three items from the current server's Brain. Items that need you come first, and tapping one opens its Session or Brain. **See all** opens the Work activity sheet, which you can also reach by pulling down.
5. Filters: **All**, **Running**, and **Needs you** (blocked or failed). When a filter matches nothing, the empty state offers **Clear filters**.

Sessions are grouped by directory into inset rounded cards. Long press still enters multi-select for termination.

All overview data comes from the current server, and the filter resets when you switch servers.

## Session screen

The header is three glass capsules: Back, the Session identity, and actions. The identity capsule shows a status dot for running, blocked, or failed Sessions, and tapping it opens Session details. Session actions open as a bottom sheet with **Terminate** last. Empty, loading, error, and "chat view unavailable" states use one chat-canvas empty state. A fresh Session shows **Ready** along with its workspace path.

## Shared patterns

- **Overflow menus** always use the bottom-sheet `ActionMenu`. This covers Brain, Sessions, Session, Work detail, and per-server actions in Settings.
- **Destructive actions** go through `confirmDestructive`, a native alert where the destructive button is never the default. This covers terminating Sessions, removing a server, deleting a Work item, and deleting a Provider.
- **Status** uses `StatusPill`. Recoverable problems and in-flow status use `InlineNotice`, and empty, loading, and blocking error states use `EmptyState`.
- **Settings** uses grouped `ListSection` / `ListRow` rows. Tapping a server opens its actions: Use, Connect, Disconnect or Retry, then Edit, then Remove.
