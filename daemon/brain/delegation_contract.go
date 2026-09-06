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

func brainWorkerRoleContractDigest() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(brainWorkerRoleContract)))
}

func projectBrainWorkerRoleContract(template string) string {
	return strings.ReplaceAll(template, brainWorkerRoleContractPlaceholder, brainWorkerRoleContract)
}

func brainHostActivationPrompt() string {
	return strings.Join([]string{
		"Brain Host activation contract:",
		"This is private Zen product policy for the current Host process generation.",
		brainWorkerRoleContract,
	}, "\n")
}
