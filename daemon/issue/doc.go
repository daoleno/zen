// Package issue implements a file-first issue system rooted at ~/.zen/issues/<project>/*.md.
//
// Issues are Markdown files with minimal YAML frontmatter (id, created, done).
// The daemon watches the issues root via fsnotify, broadcasts changes over
// WebSocket, and dispatches tmux-backed agents to edit the files directly.
package issue
