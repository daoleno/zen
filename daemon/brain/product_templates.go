package brain

import _ "embed"

// These provider-neutral templates are release assets. Fresh Brain homes and
// managed-block repair derive product behavior from these repository files.
// soul.md is embedded only as the public default for a missing or empty private
// overlay; runtime private content is never embedded in prompts or release assets.

//go:embed templates/soul.md
var defaultSoulPrinciples string

//go:embed templates/AGENTS.md
var productWorkspaceInstructions string

//go:embed templates/policies/delegation.md
var productDelegationPolicy string

//go:embed templates/policies/engine.md
var productEnginePolicy string

//go:embed templates/policies/handoff.md
var productHandoffPolicy string
