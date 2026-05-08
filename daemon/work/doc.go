// Package work implements a file-first work log rooted at ~/.zen/work/<workspace>/*.md.
//
// Items are Markdown files with minimal YAML frontmatter (id, created,
// started, done). The daemon watches the work root via fsnotify, broadcasts
// changes over WebSocket, and starts tmux-backed agents to edit the files
// directly.
package work
