package brain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	brainAgentsManagedID     = "agents"
	delegationManagedID      = "policy-delegation"
	executorManagedID        = "policy-executor"
	handoffManagedID         = "policy-handoff"
	brainFlowsManagedID      = "playbook-brain-flows-routing"
	managedMarkerPrefix      = "<!-- zen:brain-managed:"
	managedMarkerStartSuffix = ":start -->"
	managedMarkerEndSuffix   = ":end -->"
)

type managedMarkdownSpec struct {
	path         string
	relativePath string
	managedID    string
	canonical    string
}

type workspaceWritePlan struct {
	path         string
	relativePath string
	data         []byte
}

type byteSpan struct {
	start int
	end   int
}

func (s *Store) managedMarkdownSpecs() []managedMarkdownSpec {
	return []managedMarkdownSpec{
		{path: s.workspaceInstructionsPath(), relativePath: "AGENTS.md", managedID: brainAgentsManagedID, canonical: productWorkspaceInstructions},
		{path: s.policyPath("delegation.md"), relativePath: "policies/delegation.md", managedID: delegationManagedID, canonical: productDelegationPolicy},
		{path: s.policyPath("engine.md"), relativePath: "policies/engine.md", managedID: executorManagedID, canonical: productEnginePolicy},
		{path: s.policyPath("handoff.md"), relativePath: "policies/handoff.md", managedID: handoffManagedID, canonical: productHandoffPolicy},
	}
}

func managedStartMarker(id string) string {
	return managedMarkerPrefix + id + managedMarkerStartSuffix
}

func managedEndMarker(id string) string {
	return managedMarkerPrefix + id + managedMarkerEndSuffix
}

func canonicalManagedBlock(spec managedMarkdownSpec) []byte {
	return []byte(managedStartMarker(spec.managedID) + "\n" +
		strings.TrimSpace(spec.canonical) + "\n" +
		managedEndMarker(spec.managedID))
}

func (s *Store) reconcileManagedWorkspace() error {
	plans, err := s.planManagedWorkspaceReconciliation()
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if err := writeAtomic(plan.path, plan.data, 0o600); err != nil {
			return fmt.Errorf("reconcile Brain workspace %s: %w", plan.relativePath, err)
		}
	}
	return nil
}

func (s *Store) planManagedWorkspaceReconciliation() ([]workspaceWritePlan, error) {
	plans := make([]workspaceWritePlan, 0, 6)
	for _, spec := range s.managedMarkdownSpecs() {
		current, exists, err := readOptionalFile(spec.path)
		if err != nil {
			return nil, fmt.Errorf("read Brain workspace %s: %w", spec.relativePath, err)
		}
		updated, err := reconcileManagedMarkdown(current, exists, spec)
		if err != nil {
			return nil, fmt.Errorf("reconcile Brain workspace %s: %w", spec.relativePath, err)
		}
		if !exists || !bytes.Equal(current, updated) {
			plans = append(plans, workspaceWritePlan{
				path: spec.path, relativePath: spec.relativePath, data: updated,
			})
		}
	}

	profilePath := s.profileNotesPath()
	currentProfile, profileExists, err := readOptionalFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("read Brain workspace profile.md: %w", err)
	}
	updatedProfile := currentProfile
	if !profileExists || len(currentProfile) == 0 {
		updatedProfile = []byte(defaultProfileNotes)
	}
	if !profileExists || !bytes.Equal(currentProfile, updatedProfile) {
		plans = append(plans, workspaceWritePlan{
			path: profilePath, relativePath: "profile.md", data: updatedProfile,
		})
	}

	soulPath := s.soulPath()
	currentSoul, soulExists, err := readOptionalFile(soulPath)
	if err != nil {
		return nil, fmt.Errorf("read Brain workspace soul.md: %w", err)
	}
	updatedSoul := currentSoul
	if !soulExists || len(currentSoul) == 0 {
		updatedSoul = []byte(defaultSoulPrinciples)
	}
	if !soulExists || !bytes.Equal(currentSoul, updatedSoul) {
		plans = append(plans, workspaceWritePlan{
			path: soulPath, relativePath: "soul.md", data: updatedSoul,
		})
	}
	return plans, nil
}

func readOptionalFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

func reconcileManagedMarkdown(current []byte, exists bool, spec managedMarkdownSpec) ([]byte, error) {
	block := canonicalManagedBlock(spec)
	if !exists {
		return append(append([]byte{}, block...), '\n'), nil
	}

	spans, marked, err := managedBlockSpans(current, spec)
	if err != nil {
		return nil, err
	}
	if !marked {
		return appendManagedBlock(current, block), nil
	}

	var out bytes.Buffer
	cursor := 0
	for index, span := range spans {
		out.Write(current[cursor:span.start])
		if index == 0 {
			out.Write(block)
		}
		cursor = span.end
	}
	out.Write(current[cursor:])
	return out.Bytes(), nil
}

func managedBlockSpans(raw []byte, spec managedMarkdownSpec) ([]byteSpan, bool, error) {
	if !bytes.Contains(raw, []byte(managedMarkerPrefix)) {
		return nil, false, nil
	}
	startMarker := managedStartMarker(spec.managedID)
	endMarker := managedEndMarker(spec.managedID)
	spans := []byteSpan{}
	inside := false
	startOffset := 0

	for offset := 0; offset < len(raw); {
		lineEnd := bytes.IndexByte(raw[offset:], '\n')
		nextOffset := len(raw)
		if lineEnd >= 0 {
			nextOffset = offset + lineEnd + 1
		}
		body := raw[offset:nextOffset]
		body = bytes.TrimSuffix(body, []byte("\n"))
		body = bytes.TrimSuffix(body, []byte("\r"))
		line := string(body)
		if strings.Contains(line, managedMarkerPrefix) {
			switch line {
			case startMarker:
				if inside {
					return nil, true, fmt.Errorf("nested managed start marker")
				}
				inside = true
				startOffset = offset
			case endMarker:
				if !inside {
					return nil, true, fmt.Errorf("managed end marker has no matching start")
				}
				inside = false
				spans = append(spans, byteSpan{start: startOffset, end: offset + len(body)})
			default:
				return nil, true, fmt.Errorf("foreign or corrupt managed marker %q", line)
			}
		}
		offset = nextOffset
	}
	if inside {
		return nil, true, fmt.Errorf("managed start marker has no matching end")
	}
	if len(spans) == 0 {
		return nil, true, fmt.Errorf("corrupt managed marker pair")
	}
	return spans, true, nil
}

func appendManagedBlock(current, block []byte) []byte {
	updated := append([]byte{}, current...)
	if len(updated) > 0 && !bytes.HasSuffix(updated, []byte("\n")) {
		updated = append(updated, '\n')
	}
	if len(updated) > 0 && !bytes.HasSuffix(updated, []byte("\n\n")) {
		updated = append(updated, '\n')
	}
	updated = append(updated, block...)
	return append(updated, '\n')
}

func standardWorkspaceRelativePaths() []string {
	paths := []string{
		"AGENTS.md",
		"current.md",
		"memory.md",
		"profile.md",
		"soul.md",
		"policies/delegation.md",
		"policies/engine.md",
		"policies/handoff.md",
		"worklog/README.md",
	}
	paths = append(paths, seedPlaybookPaths()...)
	return paths
}
