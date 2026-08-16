package modelprofiles_test

// Shared helpers for external-package Codex live proofs (real installed CLI
// against loopback Zen-shaped endpoints). They live here so the scripted
// upstream probes and CLI proofs do not duplicate env scrubbing / tail
// trimming logic.

import "strings"

func trimTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func scrubEnv(env []string, drop ...string) []string {
	ban := map[string]struct{}{}
	for _, k := range drop {
		ban[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if _, ok := ban[key]; ok {
			continue
		}
		out = append(out, e)
	}
	return out
}