package skills

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

func BuildPluginMutationCommand(options InventoryOptions, request PluginMutationRequest, runtime PluginRuntime) (PluginMutationCommand, error) {
	if err := ValidatePluginScope(request.Scope); err != nil {
		return PluginMutationCommand{}, err
	}
	if err := ValidatePluginHost(request.Host); err != nil {
		return PluginMutationCommand{}, err
	}
	if err := ValidatePluginID(request.PluginID); err != nil {
		return PluginMutationCommand{}, err
	}
	if runtime == nil {
		runtime = NewPluginRuntime()
	}
	switch request.Operation {
	case PluginOperationInstall:
		inventory, err := DiscoverPluginInventory(options, runtime)
		if err != nil {
			return PluginMutationCommand{}, err
		}
		for _, entry := range inventory.Available {
			if entry.Host != request.Host || entry.PluginID != request.PluginID {
				continue
			}
			if !entry.Installable {
				return PluginMutationCommand{}, errors.New("the Plugin is already installed on this server")
			}
			name := entry.DisplayName
			if name == "" {
				name = entry.Name
			}
			return PluginMutationCommand{
				Operation: PluginOperationInstall, PluginID: entry.PluginID,
				Host: entry.Host, Scope: "user", Name: entry.Name,
				DisplayName: name, Agents: []Agent{pluginHostAgent(entry.Host)},
				Summary: "Install " + name + " for " + pluginHostLabel(entry.Host),
			}, nil
		}
		return PluginMutationCommand{}, errors.New("the Plugin identity is not available from the selected Agent manager")
	case PluginOperationUninstall:
		copy, err := resolvePluginCopy(options, request, runtime)
		if err != nil {
			return PluginMutationCommand{}, err
		}
		name := copy.DisplayName
		if name == "" {
			name = copy.Name
		}
		return PluginMutationCommand{
			Operation: PluginOperationUninstall, PluginID: copy.PluginID,
			Host: copy.Host, Source: copy.Source, Scope: copy.Scope, CopyID: copy.CopyID,
			Name: copy.Name, DisplayName: name, RootPath: copy.RootPath,
			Version:       copy.Version,
			CanonicalPath: copy.CanonicalPath, AllowedRoot: copy.AllowedRoot,
			Location: copy.Location, Revision: copy.Revision,
			Agents:      append([]Agent{}, copy.Agents...),
			Summary:     "Permanently uninstall " + name + " from " + copy.Location,
			Destructive: true,
		}, nil
	default:
		return PluginMutationCommand{}, fmt.Errorf("unsupported Plugin operation %q", request.Operation)
	}
}

func ExecutePluginMutationCommand(ctx context.Context, command PluginMutationCommand, options MutationExecutionOptions) (MutationExecution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime := options.PluginRuntime
	if runtime == nil {
		runtime = NewPluginRuntime()
	}
	timeout := options.Timeout
	if timeout <= 0 {
		if command.Operation == PluginOperationUninstall {
			timeout = DefaultRemovalTimeout
		} else {
			timeout = DefaultMutationTimeout
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	options.Timeout = timeout
	options.PluginRuntime = runtime
	options.InventoryOptions.Context = runCtx

	before, err := DiscoverPluginInventory(options.InventoryOptions, runtime)
	if err != nil {
		return MutationExecution{}, err
	}
	var target InstalledPluginCopy
	if command.Operation == PluginOperationUninstall {
		target, err = resolvePluginCopy(options.InventoryOptions, pluginRequestFromCommand(command), runtime)
		if err != nil {
			return MutationExecution{}, err
		}
	}

	args, err := pluginMutationArgs(command)
	if err != nil {
		return MutationExecution{}, err
	}
	execution, execErr := runtime.Execute(runCtx, command.Host, args, options)
	if errors.Is(runCtx.Err(), context.Canceled) {
		return MutationExecution{}, ErrMutationCancelled
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return execution, ErrMutationTimedOut
	}
	if execErr != nil || !execution.Success {
		after, verifyErr := DiscoverPluginInventory(options.InventoryOptions, runtime)
		if verifyErr == nil {
			verifyErr = verifyPluginInventoryUnchanged(before, after)
		}
		if verifyErr != nil {
			integrityErr := fmt.Errorf("Plugin manager changed installed state after reporting failure: %w", verifyErr)
			if execErr != nil {
				return execution, errors.Join(execErr, integrityErr)
			}
			return execution, integrityErr
		}
		if execErr != nil {
			return execution, execErr
		}
		return execution, nil
	}

	after, err := DiscoverPluginInventory(options.InventoryOptions, runtime)
	if err != nil {
		return MutationExecution{}, fmt.Errorf("verify Plugin mutation: %w", err)
	}
	switch command.Operation {
	case PluginOperationInstall:
		if !inventoryHasPlugin(after, command.Host, command.PluginID) {
			return MutationExecution{}, errors.New("Plugin manager reported success, but the installed copy did not appear")
		}
	case PluginOperationUninstall:
		if inventoryHasCopy(after, target.CopyID) {
			return MutationExecution{}, errors.New("Plugin manager reported success, but the selected copy is still installed")
		}
		if err := verifyPluginNeighbors(before, after, target.CopyID); err != nil {
			return MutationExecution{}, err
		}
	}
	if strings.TrimSpace(execution.Output) == "" {
		verb := "Installed "
		if command.Operation == PluginOperationUninstall {
			verb = "Uninstalled "
		}
		name := command.DisplayName
		if name == "" {
			name = command.Name
		}
		execution.Output = verb + name + "."
	}
	return execution, nil
}

func resolvePluginCopy(options InventoryOptions, request PluginMutationRequest, runtime PluginRuntime) (InstalledPluginCopy, error) {
	if request.Operation != PluginOperationUninstall {
		return InstalledPluginCopy{}, errors.New("an exact installed Plugin copy is required")
	}
	if request.CopyID == "" || len(request.CopyID) != 24 {
		return InstalledPluginCopy{}, errors.New("invalid Plugin copy ID")
	}
	if ValidatePluginSource(request.Source) != nil || request.Source != PluginSourceManager {
		return InstalledPluginCopy{}, errors.New("invalid uninstallable Plugin source")
	}
	name, _, ok := splitPluginID(request.PluginID)
	if !ok || request.Name != name {
		return InstalledPluginCopy{}, errors.New("Plugin name does not match its manager identity")
	}
	if request.Revision == "" || len(request.Revision) != 64 {
		return InstalledPluginCopy{}, errors.New("invalid Plugin revision")
	}
	if cleanPluginVersion(request.Version) != request.Version {
		return InstalledPluginCopy{}, errors.New("invalid Plugin version")
	}
	if !slices.Equal(request.Agents, []Agent{pluginHostAgent(request.Host)}) {
		return InstalledPluginCopy{}, errors.New("Plugin Agent identity does not match its host")
	}
	for field, value := range map[string]string{
		"root path": request.RootPath, "canonical path": request.CanonicalPath, "allowed root": request.AllowedRoot,
	} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return InstalledPluginCopy{}, fmt.Errorf("invalid Plugin %s", field)
		}
	}
	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		return InstalledPluginCopy{}, err
	}
	for _, copy := range inventory.Installed {
		if copy.CopyID != request.CopyID || copy.PluginID != request.PluginID || copy.Name != request.Name || copy.Host != request.Host {
			continue
		}
		if copy.Source != request.Source || copy.Version != request.Version || !slices.Equal(copy.Agents, request.Agents) || copy.RootPath != request.RootPath || copy.CanonicalPath != request.CanonicalPath || copy.AllowedRoot != request.AllowedRoot || copy.Revision != request.Revision {
			return InstalledPluginCopy{}, fmt.Errorf("the selected copy of Plugin %q changed after it was loaded", request.Name)
		}
		if !copy.Capability.CanUninstall {
			reason := strings.TrimSpace(copy.Capability.Reason)
			if reason == "" {
				reason = "this Plugin copy cannot be uninstalled from here"
			}
			return InstalledPluginCopy{}, errors.New(reason)
		}
		if err := validatePluginCopyIdentity(copy); err != nil {
			return InstalledPluginCopy{}, err
		}
		return copy, nil
	}
	return InstalledPluginCopy{}, fmt.Errorf("the selected copy of Plugin %q is stale or no longer installed", request.Name)
}

func pluginRequestFromCommand(command PluginMutationCommand) PluginMutationRequest {
	return PluginMutationRequest{
		Operation: command.Operation, PluginID: command.PluginID, Host: command.Host,
		Source: command.Source, Scope: command.Scope, CopyID: command.CopyID, Name: command.Name,
		Version:  command.Version,
		RootPath: command.RootPath, CanonicalPath: command.CanonicalPath,
		AllowedRoot: command.AllowedRoot, Revision: command.Revision,
		Agents: append([]Agent{}, command.Agents...),
	}
}

func pluginMutationArgs(command PluginMutationCommand) ([]string, error) {
	if ValidatePluginID(command.PluginID) != nil || ValidatePluginHost(command.Host) != nil || ValidatePluginScope(command.Scope) != nil {
		return nil, ErrMutationCommandInvalid
	}
	switch command.Operation {
	case PluginOperationInstall:
		if command.Host == PluginHostClaude {
			return []string{"plugin", "install", command.PluginID, "--scope", "user"}, nil
		}
		return []string{"plugin", "add", command.PluginID, "--json"}, nil
	case PluginOperationUninstall:
		if command.CopyID == "" || command.Revision == "" || command.Source != PluginSourceManager || cleanPluginVersion(command.Version) != command.Version || !slices.Equal(command.Agents, []Agent{pluginHostAgent(command.Host)}) {
			return nil, ErrMutationCommandInvalid
		}
		if command.Host == PluginHostClaude {
			return []string{"plugin", "uninstall", command.PluginID, "--scope", "user", "--yes"}, nil
		}
		return []string{"plugin", "remove", command.PluginID, "--json"}, nil
	default:
		return nil, ErrMutationCommandInvalid
	}
}

func PluginMutationTimeoutForOperation(args []string) time.Duration {
	for _, arg := range args {
		if arg == "uninstall" || arg == "remove" {
			return DefaultRemovalTimeout
		}
	}
	return DefaultMutationTimeout
}

func inventoryHasPlugin(inventory PluginInventory, host PluginHost, pluginID string) bool {
	for _, copy := range inventory.Installed {
		if copy.Host == host && copy.PluginID == pluginID && copy.Source == PluginSourceManager {
			return true
		}
	}
	return false
}

func inventoryHasCopy(inventory PluginInventory, copyID string) bool {
	for _, copy := range inventory.Installed {
		if copy.CopyID == copyID {
			return true
		}
	}
	return false
}

func verifyPluginNeighbors(before, after PluginInventory, removedCopyID string) error {
	expected := pluginInventoryFingerprints(before, removedCopyID)
	actual := pluginInventoryFingerprints(after, "")
	if len(expected) != len(actual) {
		return errors.New("Plugin manager changed another installed Plugin copy")
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return errors.New("Plugin manager changed another installed Plugin copy")
		}
	}
	return nil
}

func verifyPluginInventoryUnchanged(before, after PluginInventory) error {
	expected := pluginInventoryFingerprints(before, "")
	actual := pluginInventoryFingerprints(after, "")
	if !slices.Equal(expected, actual) {
		return errors.New("installed Plugin inventory changed")
	}
	return nil
}

func pluginInventoryFingerprints(inventory PluginInventory, excludedCopyID string) []string {
	fingerprints := make([]string, 0, len(inventory.Installed))
	for _, copy := range inventory.Installed {
		if copy.CopyID == excludedCopyID {
			continue
		}
		fingerprints = append(fingerprints, copy.CopyID+"\x00"+copy.Revision)
	}
	sort.Strings(fingerprints)
	return fingerprints
}
