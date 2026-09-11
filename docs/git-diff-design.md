# Git Diff Redesign

Product and interaction specification for the Git Diff review surface opened from
a Terminal. This document is the source of truth for information hierarchy,
navigation, scrolling, safe-area and accessibility behavior. It accompanies
`docs/git-review.md` (data/protocol semantics) and does not replace it.

## 1. Existing Problems

The previous surface stacked two independent mode systems and then changed its
own chrome while the user was reading.

1. **Stacked modes.** A top `Diff N | Files` switch sat above a second
   `All | Working | Staged` scope row. Two rows of navigation competed for a
   phone header and neither was clearly primary.
2. **Chrome jumped on review.** Selecting a file turned on a `compact` header
   that removed the mode row and reflowed the sheet. The layout changed at the
   exact moment the user was trying to read.
3. **Bottom scroll rebound (user-reported).** The changed-file `FlatList` passed
   a live `contentOffset={{ x: 0, y: overviewOffset.current }}` prop. On Android
   Fabric, `ReactScrollView.setContentOffset` calls `scrollTo()` whenever the
   prop value changes, and `VirtualizedList` re-renders (and re-passes the prop)
   while it mounts cells during a scroll. The ref was fed by a throttled
   `onScroll`, so mid-fling the list was told to scroll back to an older offset.
   This cancelled momentum and settled short of the true bottom, leaving the
   last changed file partly off-screen and repeatedly "springing back". This was
   a controlled-scroll bug, not a cosmetic padding problem.
4. **Unstable file rows.** Rows showed one `path` plus a status line. Long paths
   ellipsized the filename, directory context was lost, and there was no status
   glyph, so scanning a large change set was slow.
5. **Nested modal for options.** Wrap/text-size/patch-header controls opened a
   second `Modal` on top of the sheet.
6. **Back was ambiguous.** Android hardware back always closed the whole sheet,
   even from a working-file preview or the Files browser.

## 2. Goals

- One clear hierarchy: repository context, then scope, then files, then diff.
- Stable chrome. Header height and controls do not move when entering a file.
- The changed-file list always reaches its real bottom. The last row is fully
  visible and tappable after every gesture and after momentum settles.
- Predictable Back on Android and iOS: it unwinds one level at a time.
- Efficient review: list -> detail -> back without toolbar piles or nested
  modals.
- A real wide layout for tablets/foldables/wide web, not a stretched phone.
- Keep every existing data guarantee: paging/stream limits, staged/working/
  untracked semantics, truncation, binary, rename, error and retry.

## 3. Information Hierarchy

From highest to lowest salience:

1. **Repository + branch** (`repo · branch`) — always visible in the overview
   header; never inferred or aggregated across servers. Backed by the current
   server's snapshot only.
2. **Scope** (`All`, `Working`, `Staged`) with file counts — one segmented
   control, the single filter owner.
3. **Summary** (`N files  +a  -d`) for the selected scope — one muted line.
4. **Files** — a virtualized list with per-file status and change counts.
5. **Diff** — the selected file's unified patch.
6. **Options/search** — progressive disclosure, never permanent rows.

The surface never shows two mode bars. `Files` (working-tree browser) is a
secondary destination reached from the overview header, not a peer tab.

### Screen states

The sheet has one of four mutually exclusive states:

| State | Primary content | Header left | Header title |
| --- | --- | --- | --- |
| `overview` | Changed-file list | Close | `Changes` + repo·branch |
| `reader` | Unified diff for one file | Back to list | filename + status |
| `browser` | Working-tree directory list | Back to changes | `Files` + repo·branch |
| `file` | Working-tree file snapshot | Back to origin | filename + directory |

Transitions: `overview -> reader` (tap file), `reader -> overview` (Back),
`overview -> browser` (browse icon), `browser -> file` (tap file),
`file -> browser` (Back), `browser -> overview` (Back).
`reader -> file` is reachable from the diff options ("Open working file"); its
Back returns to `reader`. A working file opened from the browser returns to the
browser instead. The header Back, Android hardware back and any content Back all
resolve through one origin-aware back stack, so the same file never lands in two
different places depending on which control is used.

## 4. Phone: List -> Detail

- **Overview** owns the header, the scope control and the filter input.
- Tapping a row opens the reader. The overview list stays mounted underneath
  (see section 8) so its scroll position is retained without replay.
- **Reader** replaces the list visually. Its header shows a back control with
  accessibility label `Changed files`, the filename, and the status line. It
  keeps the search and options icon controls in fixed 44dp lanes.
- **Android hardware back** and iOS back unwind the state stack in order:
  `file -> origin` (`reader` when opened from the diff options, `browser` when
  opened from the browser), `browser -> overview`, `reader -> overview`,
  `overview -> close sheet`. Back must never close the feature from `reader`,
  `browser` or `file`. Only one control owns file Back; the file view renders no
  second title bar.
- Closing the sheet clears transient review state; reopening starts at
  `overview`.

## 5. Wide Layout

At a window width of at least **720dp**, the changes review becomes a
two-pane master-detail:

- Left pane: fixed 320dp master with the repo/list header (title, repo·branch,
  filter, browse, refresh), the scope control and the list. It stays visible and
  interactive while a file is selected, so list filtering never requires leaving
  the reader.
- Right pane: an independent detail pane with its own file header (name, status,
  search, options, refresh) above the reader. When nothing is selected it shows a
  deliberate empty invitation state; it is not an absent pane.
- Clearing the selection in the detail header returns to the empty detail state
  without leaving the feature.
- Phones keep the single contextual header; the split detail header is a
  wide-layout affordance, so no stacked bars appear on narrow screens.

Below 720dp the phone swap behavior in section 4 applies. The `browser` and
`file` states remain full-width on wide layouts until a wide browser is
designed; this is an explicit, documented limitation, not a silent fallback.

## 6. Stable Repository Context

- The subtitle is `repo_name · branch` when both exist. With no branch it is the
  repository name, falling back to the repo-root basename, then `Repository`; the
  pre-snapshot placeholder is `Diff and files`.
- The value comes from the current server's `GitDiffStatusSnapshot`. It never
  aggregates multiple servers and never shows a stale owner after a server,
  session or cwd switch (`ownerKey` invalidates the previous snapshot).
- The overview subtitle is single-line and tail-truncates. The working-file path
  is single-line and head-truncates so the nearest directory stays visible.
  Neither reflows the header.

## 7. Scope, Filter, Counts

- Scope is one segmented control: `All N`, `Working N`, `Staged N`.
  - `All` = staged + working + untracked.
  - `Working` = index vs working tree, including untracked as additions.
  - `Staged` = HEAD vs index, including an unborn index.
- The scope tab counts are the number of files in each comparison and do not
  change with the path filter. The list meta line shows `N files`, where `N` is
  the selected comparison's count, or `M / N files` while a filter is active
  (`M` matches within that comparison out of `N`).
- Per-file stats use the selected scope's own numbers (`gitDiffCounts`), never a
  sum of index and working changes.
- The filter matches destination path and rename source, case-insensitively.
  Filtering resets the list to the top exactly once.
- Changing scope resets the list to the top exactly once.

## 8. Scroll Retention, Reset, Safe Area, Keyboard

### Retention

- The overview `FlatList` is **never unmounted** while the sheet is open.
  `reader`/`file`/`browser` are drawn above it, so the native list keeps its
  true offset and no pixel/row replay is needed when returning.
- The reader keeps one saved position per `(path, scope)`: a source row plus a
  pixel remainder plus an optional horizontal offset, plus a layout signature
  (`wrap:fontSize:fontScale`). Returning to a file restores that position. A
  layout change (wrap/text-size/font scale) re-anchors to the visible source
  row instead of the old pixel offset.
- Switching to the Files browser and back preserves both the overview offset
  and the reader position.

### No controlled offset

- The overview list must **not** receive a live `contentOffset` prop. Initial
  scrolling is imperative only. This is the fix for the reported rebound:
  `contentOffset` on Android re-applies `scrollTo` on prop change, and
  `VirtualizedList` re-renders during scroll.
- The only programmatic list scroll is `scrollToOffset({ offset: 0 })` on scope
  or filter change.

### Reset

- Scope change, filter change, sheet reopen and owner change reset the overview
  to the top.
- Refresh keeps the overview offset and reloads the reader at the top of the
  selected file (the reader remounts on every `refreshKey`). Paging with an older
  content version surfaces the explicit changed state instead of mixing old and
  new rows. Within an unchanged reader, leaving to a detail and returning
  restores the saved source-row position.

### Safe area

- The sheet owns the top inset with `SafeAreaView edges={["top"]}`.
- Scroll content owns the bottom inset: list and reader content padding includes
  `insets.bottom`, so the final row can scroll fully above the navigation bar or
  home indicator and remains tappable. Padding is the real system inset plus a
  small constant breathing gap; it is not a fixed magic value.
- The last row must remain fully visible after a fling and after momentum
  settles, including on a 3-button navigation bar and under edge-to-edge.

### Keyboard

- The filter input and the in-diff search sit at the top of the content, so the
  keyboard does not cover them.
- Android uses `softwareKeyboardLayoutMode: resize`; the list shrinks by the
  keyboard height and keeps its offset. Dismissing the keyboard restores the
  viewport without scroll jumps.
- Taps on rows persist while the keyboard is open
  (`keyboardShouldPersistTaps="handled"`).

## 9. File Rows

Each row shows, left to right:

1. A status glyph (Ionicons) colored by status:
   - `added` -> `add-circle-outline` (green)
   - `deleted` -> `remove-circle-outline` (red)
   - `renamed` / `copied` -> `swap-horizontal-outline` (blue)
   - `modified` / `changed` -> `ellipse-outline` (yellow)
   - `conflict` -> `warning-outline` (red)
   - `untracked` -> `cloud-upload-outline` (muted)
   - binary patch -> `cube-outline` (muted)
2. Filename on the first line, medium weight, one line, tail-truncated.
3. Directory on the second line, muted mono, one line, **head**-truncated so the
   nearest parent directory stays visible.
4. Status words on the second line: `Staged`, `Unstaged`, `Staged + unstaged`,
   `Untracked`, and `from <old>` for renames.
5. Right-aligned `+a` / `-d` in the scope's numbers, or `Binary`.

Rows are at least 56dp tall, use a full-width press target, and carry an
accessibility label of the form
`<path>, <status>, <scope description>, plus <a>, minus <d>` (or `binary`).

## 10. Unified Diff Reader

- Old and new line numbers in a fixed gutter, colored change rows, hunk
  headers, and explicit continuation markers for wrapped long lines.
- Hunk and full-comparison search: case-insensitive, navigates matching lines
  and hunks with previous/next controls; the complete comparison is searched,
  not just the loaded page.
- Inline options (no nested modal): wrap lines, text size (10-20), patch
  headers. Wrap and text-size changes re-anchor to the visible source row;
  patch-header changes re-render the header rows immediately.
- Git headers (`diff --git`, `index`, `---`, `+++`) are hidden by default and
  revealed by the patch-header option or while searching.
- Binary, mode-only and rename metadata are shown as metadata rows, never as
  fake code.
- Truncation, stale-version, error and retry states are explicit and
  non-destructive: already-loaded rows stay readable.
- "Open working file" opens the file state and Back returns to the reader.

## 11. Loading, Empty, Error, Large

- **Loading:** a centered busy state on first load; a small pending indicator at
  the loaded edge during pagination. The list/reader never blanks.
- **Clean/empty:** `Working tree is clean` for a clean repository;
  `No matching changes` for an empty filter; `No changes in this comparison`
  for an empty scope.
- **Error:** an inline card with a retry control. Errors are visible even after
  an earlier success.
- **Large diffs:** the reader keeps the existing bounded paging (120 rows per
  request, 512 UTF-8 bytes per row), contiguous chunks, and virtualized list.
  The overview virtualizes 100+ rows with `initialNumToRender`, `windowSize`
  and batched rendering; no flattening and no full-patch retention.

## 12. Accessibility

- Icon-only controls have `accessibilityRole="button"`, an
  `accessibilityLabel`, and `accessibilityState` for selected/disabled. A long
  press surfaces the label as a tooltip (`Alert`) where the platform has no
  native tooltip.
- All interactive targets are at least 44x44dp.
- Selected scope uses `accessibilityRole="tab"` with
  `accessibilityState={{ selected }}`.
- Diff rows expose old/new line numbers for screen readers.
- Dimension-critical rows avoid height changes on selection so focus and scroll
  stay stable.

## 13. Non-Goals

- No staging, commit, discard, checkout or any repository mutation.
- No arbitrary commit/branch comparison beyond `All`/`Working`/`Staged`.
- No new design system, component library or dependency.
- No unrelated terminal, navigation or Files-browser redesign.

## 14. Acceptance Matrix

| ID | Scenario | Pass condition |
| --- | --- | --- |
| A1 | Flick changed-file list to bottom, 100+ varied rows | After momentum settles the last row is fully visible and not under any system bar |
| A2 | Repeat A1 three times | Same settled geometry each time; no upward snap |
| A3 | Slow drag to bottom | Last row fully visible; press target works |
| A4 | Tap last row | Correct file diff opens |
| A5 | Back from reader | Overview returns at the exact prior offset |
| A6 | Refresh at bottom | Overview offset retained; reader reloads at the file top or shows changed state |
| A7 | Change scope / type filter | List resets to top once; counts update per scope |
| A8 | Zero files | Clean/empty state, no list chrome |
| A9 | One file | Single row, reachable, tappable |
| A10 | Renamed / deleted / untracked / binary rows | Correct glyph, wording, stats; binary has no fake +a/-d |
| A11 | Large paginated diff | Edge loading works; search/hunk nav works; no full retention |
| A12 | Working file open and Back | Returns to reader when opened from reader, to browser when opened from browser; reader position retained |
| A13 | Android hardware back from reader/browser/file | Unwinds one level; never closes from a child state; matches the header Back for the same state |
| A14 | 360x800 and larger phone, light and dark | Header height stable; >=44dp targets; readable contrast |
| A15 | Wide viewport >=720dp | Two-pane master-detail; list header/filter/browse stay usable while a file is selected |
| A16 | Large system font / fontScale | Gutter and rows do not clip or overlap |
| A17 | Keyboard open / dismiss | Input visible; list offset stable; taps work; focus returns to a visible control |
| A18 | Orientation / viewport resize | Layout remains valid; anchored reader re-anchors to source row |
| A19 | Wide, no selection | Deliberate empty detail pane; selecting then clearing returns to it |
| A20 | Wide, filter while a file is open | List filters in place; detail stays selected/open |

## 15. Verification Notes

- Deterministic Bun tests cover the navigation state machine, file metadata
  derivation, filter/count semantics, and the overview scroll contract
  (no controlled `contentOffset`, persistent mount, explicit reset).
- The actual components are rendered for visual evidence at narrow phone,
  larger phone and wide viewports, in light and dark, including a
  bottom-settled geometry check.
- Web/Playwright evidence proves layout and hierarchy only. Native Android
  scroll physics and safe-area behavior require an emulator or device; if a
  native runtime is unavailable, that limitation is reported explicitly rather
  than inferred from the web build.
