# Git Review

Git Diff in a terminal reviews changes on that terminal's current server and
working directory. Android and iOS share the same overview, comparisons, reader,
search, and navigation. Reviewing does not stage, discard, or otherwise modify
the repository.

## Comparisons

- **All** lists staged, working-tree, and untracked changes. A file changed in both
  index and working tree has separate, explicitly labeled patch sections.
- **Working** compares the index with the working tree and includes untracked
  files as additions.
- **Staged** compares HEAD with the index, including an initial, unborn index.

Overview counts follow the selected comparison. The top-level repository summary
includes all changes. Paths can be filtered by destination or rename source.
Git handles rename detection, binary classification of patches, submodules, and
diff generation. No arbitrary commit/branch comparison is implied by these modes.

## Reader

One file is open at a time. The unified reader shows old and new source line
numbers, change colors, hunk headers, binary/mode/rename metadata, and missing
final-newline markers. File arrows move through the filtered list. Page arrows
and hunk arrows navigate the complete patch. Positions are retained per file and
comparison for the open review, including a visit to the Files tab.

The search control submits a case-insensitive search of the complete selected
comparison, not just the current page. Its arrows navigate matching lines.
The patch-header toggle reveals redundant Git headers; those headers are also
revealed while searching. Wrap and text-size controls change readability without
refetching the patch. Long lines are split into explicitly marked continuations,
not truncated. Unwrapped pages share a horizontal scroll position.

The Files tab remains a working-tree browser, not a historical comparison. Its
existing file-preview byte limit is separate from the complete diff reader.

## Data And Freshness

Update both the mobile client and its current daemon when installing Git review.
`Unknown message type: git_diff_page` means the connected daemon predates the
paged reader. Verify the current server identity and deploy the matching daemon;
reconnecting to the same old executable cannot add the handler. After an update,
refresh the overview before reopening a file whose changes have since been committed.

`git_diff_status` uses NUL-delimited status and batched statistics, preserving
literal filenames, including whitespace and Git pathspec metacharacters.
`git_diff_page` accepts `path`, `scope` (`all`, `working`, `staged`), zero-based
`row`, optional `file_generation`, and optional `query`. It returns up to 120
display rows of at most 512 UTF-8 bytes each, total row count, neighboring hunk
and matching-line positions, and a content version.

Each page streams Git output for the selected file. The daemon scans that output
to compute navigation and a digest, but retains only the requested page. It does
not cache repository snapshots or retain entire patches for the mobile reader.
This trades a new selected-file Git invocation per page for bounded retained
memory and simple freshness semantics. Native lists virtualize the requested
rows; syntax token trees are not created for an entire file.

A reader is a snapshot. Refresh reloads the overview and selected diff. Paging
with an older content version produces an explicit changed-file state, not a
mixture of old and new content. Repository errors are visible even after an earlier
successful overview. Changing server, session, working directory, or connection
clears the previous owner's data. Late responses cannot restore it.

## Verification

Disposable Git fixtures and authenticated WebSocket regressions are in
`daemon/server/git_diff_*_test.go`. Run:

```sh
cd daemon
go test ./server -run 'GitDiff|GitRepo'
go test ./server -run '^$' -bench '^BenchmarkGitDiff$' -benchtime=3x
```

The benchmark includes small, medium, many-file, large-single-file, and long-line
repositories. `TestGitDiffNativeFixtureServer` is opt-in with
`ZEN_GIT_DIFF_NATIVE=1`; it binds only loopback port 8097 and uses the production
authenticated WebSocket handler against temporary repositories. `/stop` shuts
down this test resource. It is not a daemon entry point or a product route.

Native performance evidence must come from Android/iOS execution, not these Go
or Bun tests. Android emulator data, debug-build overhead, and software-rendered
frame timings must be distinguished from release-device performance. iOS bundles
can be exported on Linux, but iOS Simulator verification requires macOS.
