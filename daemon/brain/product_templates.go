package brain

import _ "embed"

// These provider-neutral templates are release assets. Fresh Brain homes and
// managed-block repair derive product behavior from these repository files;
// private workspace overlays are never embedded here.

//go:embed templates/AGENTS.md
var productWorkspaceInstructions string

//go:embed templates/policies/delegation.md
var productDelegationPolicy string

//go:embed templates/policies/engine.md
var productEnginePolicy string

//go:embed templates/policies/handoff.md
var productHandoffPolicy string
