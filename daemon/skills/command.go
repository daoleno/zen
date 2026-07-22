package skills

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var installedSkillIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func BuildMutationCommand(options InventoryOptions, request MutationRequest) (MutationCommand, error) {
	if err := ValidateScope(request.Scope); err != nil {
		return MutationCommand{}, err
	}
	cwd, err := ValidateCWD(request.CWD, request.Scope == ScopeProject)
	if err != nil {
		return MutationCommand{}, err
	}
	request.CWD = cwd

	switch request.Operation {
	case OperationInstall:
		return buildInstallCommand(request)
	case OperationRemove:
		return buildInstalledCommand(options, request)
	default:
		return MutationCommand{}, fmt.Errorf("unsupported Skill operation %q", request.Operation)
	}
}

func buildInstallCommand(request MutationRequest) (MutationCommand, error) {
	if err := ValidateCatalogIdentity(request.SkillID, request.Source, request.SkillName); err != nil {
		return MutationCommand{}, err
	}
	agents, err := validateAgents(request.Agents)
	if err != nil {
		return MutationCommand{}, err
	}
	parts := []string{
		"npx", "skills", "add",
		"https://github.com/" + request.Source,
		"--skill", request.SkillName,
	}
	if request.Scope == ScopeGlobal {
		parts = append(parts, "--global")
	}
	for _, agent := range agents {
		parts = append(parts, "--agent", string(agent))
	}
	parts = append(parts, "--yes")
	return MutationCommand{
		Operation: OperationInstall,
		Command:   strings.Join(parts, " "),
		CatalogID: request.SkillID,
		Source:    request.Source,
		SkillName: request.SkillName,
		Scope:     request.Scope,
		Agents:    agents,
	}, nil
}

func buildInstalledCommand(options InventoryOptions, request MutationRequest) (MutationCommand, error) {
	if !installedSkillIDPattern.MatchString(request.SkillID) {
		return MutationCommand{}, errors.New("invalid installed Skill identity")
	}
	requestedAgents, err := validateAgents(request.Agents)
	if err != nil {
		return MutationCommand{}, err
	}
	options.CWD = request.CWD
	inventory, err := DiscoverInventory(options)
	if err != nil {
		return MutationCommand{}, err
	}
	if inventory.incomplete {
		return MutationCommand{}, errors.New("installed Skills inventory is incomplete; removal is disabled")
	}
	var installed *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].ID == request.SkillID {
			installed = &inventory.Skills[index]
			break
		}
	}
	if installed == nil || installed.Scope != request.Scope {
		return MutationCommand{}, errors.New("installed Skill is not present in the requested scope")
	}
	if installed.Manager != ManagerSkillsCLI || ValidateSkillName(installed.Name) != nil {
		return MutationCommand{}, errors.New("installed Skill has no provable skills-cli ownership")
	}
	if len(installed.Bindings) == 0 {
		return MutationCommand{}, errors.New("installed Skill has no exact CLI binding")
	}
	boundAgents := make([]Agent, 0, len(installed.Agents))
	for _, binding := range installed.Bindings {
		if binding.Scope != installed.Scope || filepath.Base(binding.SourcePath) != installed.Name {
			return MutationCommand{}, errors.New("installed Skill binding does not match its CLI identity")
		}
		mergeAgents(&boundAgents, binding.Agents)
	}
	supportedInstalledAgents := make([]Agent, 0, len(installed.Agents))
	for _, agent := range installed.Agents {
		if ValidateAgent(agent) == nil {
			supportedInstalledAgents = append(supportedInstalledAgents, agent)
		}
	}
	if !sameAgentSet(boundAgents, supportedInstalledAgents) {
		return MutationCommand{}, errors.New("installed Skill bindings do not prove the requested CLI targets")
	}

	if !installed.Capability.CanRemove {
		return MutationCommand{}, errors.New("installed Skill cannot be safely removed")
	}
	if !matchesRemovalPlan(requestedAgents, installed.Capability.RemovalPlans) {
		return MutationCommand{}, errors.New("target agents do not match a provable removal plan")
	}
	parts := []string{"npx", "skills", "remove", installed.Name}
	if installed.Scope == ScopeGlobal {
		parts = append(parts, "--global")
	}
	for _, agent := range requestedAgents {
		parts = append(parts, "--agent", string(agent))
	}
	parts = append(parts, "--yes")
	return MutationCommand{
		Operation: request.Operation,
		Command:   strings.Join(parts, " "),
		SkillName: installed.Name,
		Scope:     installed.Scope,
		Agents:    requestedAgents,
	}, nil
}

func matchesRemovalPlan(requested []Agent, plans []AgentRemovalPlan) bool {
	for _, plan := range plans {
		if sameAgentSet(requested, plan.AffectedAgents) {
			return true
		}
	}
	return false
}

func sameAgentSet(left, right []Agent) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[Agent]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}
