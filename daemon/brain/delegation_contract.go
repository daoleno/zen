package brain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	brainWorkerRoleContractPlaceholder = "{{ZEN_BRAIN_WORKER_ROLE_CONTRACT}}"
	brainWorkerRoleContract            = "Brain owns conversation, planning, lifecycle, review and acceptance. Delegate substantive execution to a visible Zen Worker unless the user explicitly asks Brain to execute it directly. Inspect context as needed to form or review a brief. Questions and discussion need no Worker. A delegation failure does not authorize direct execution."
)

func brainHostContractDigest() string {
	// Refresh an existing Host when release guidance changes, not when private
	// overlays change. Hash lazy guidance without embedding it in activation.
	parts := []string{brainHostActivationPrompt(), productWorkspaceInstructions, productDelegationPolicy, productEnginePolicy, productHandoffPolicy}
	for _, playbook := range seedPlaybooks {
		parts = append(parts, playbook.name, playbook.initial)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

func projectBrainWorkerRoleContract(template string) string {
	return strings.ReplaceAll(template, brainWorkerRoleContractPlaceholder, brainWorkerRoleContract)
}

func brainHostActivationPrompt() string {
	return strings.Join([]string{
		"Brain Host activation contract:",
		"This is private Zen product policy for the current Host process generation.",
		brainWorkerRoleContract,
		"Read AGENTS.md for current product guidance before continuing, even in a resumed Session. Reload the relevant policy or playbook when needed; do not load the whole catalog. Preserve private overlays and active Work/Event state.",
	}, "\n")
}
