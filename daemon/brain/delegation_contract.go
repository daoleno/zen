package brain

import "strings"

const (
	brainWorkerRoleContractVersion     = "zen-brain-worker-role/v1"
	brainWorkerRoleContractPlaceholder = "{{ZEN_BRAIN_WORKER_ROLE_CONTRACT}}"
	brainWorkerRoleContract            = "Brain directly owns conversation, clarification, decomposition, judgment, lifecycle, review, acceptance, and synthesis. When a goal contains a substantive executable concern, Brain must create or reuse a visible Zen Worker before implementation or tool-backed verification. Brain may inspect enough context to form or review the brief, but speed, convenience, task coherence, or perceived simplicity are not reasons to absorb Worker execution. If there is no executable concern, Brain does not delegate. An executor or platform restriction that prevents creating a Zen Worker is a blocker to report, not permission for Brain to implement the concern."
)

func projectBrainWorkerRoleContract(template string) string {
	return strings.ReplaceAll(template, brainWorkerRoleContractPlaceholder, brainWorkerRoleContract)
}

func brainHostActivationPrompt() string {
	return strings.Join([]string{
		"Brain Host activation contract:",
		"Version: " + brainWorkerRoleContractVersion,
		"This is private Zen product policy for the current Host process generation.",
		brainWorkerRoleContract,
	}, "\n")
}
