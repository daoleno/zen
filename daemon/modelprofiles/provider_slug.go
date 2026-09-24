package modelprofiles

import (
	"fmt"
	"regexp"
	"strings"
)

var slugWordRE = regexp.MustCompile(`[^a-z0-9]+`)

// ProviderSlugFor creates a readable, collision-resistant slug. The canonical
// connection ID is included so deletion never permits a later connection to
// silently inherit the old public identity.
func ProviderSlugFor(name, id string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = slugWordRE.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "provider"
	}
	id = normalizeID(id)
	if len(id) > 8 {
		id = id[:8]
	}
	base = strings.Trim(strings.Join([]string{base, id}, "-"), "-")
	if len(base) > 48 {
		base = strings.TrimRight(base[:48], "-")
	}
	if err := ValidateProviderSlug(base); err != nil {
		fallback := slugWordRE.ReplaceAllString("provider-"+id, "-")
		fallback = strings.Trim(fallback, "-")
		if len(fallback) > 48 {
			fallback = strings.TrimRight(fallback[:48], "-")
		}
		return fallback
	}
	return base
}

func ensureProviderSlug(profile Profile, profiles map[string]Profile) (Profile, error) {
	profile = normalizeProfile(profile)
	if profile.Slug == "" {
		profile.Slug = ProviderSlugFor(profile.Name, profile.ID)
	}
	if err := ValidateProviderSlug(profile.Slug); err != nil {
		return Profile{}, err
	}
	for id, existing := range profiles {
		if id != profile.ID && strings.EqualFold(existing.Slug, profile.Slug) {
			return Profile{}, fmt.Errorf("%w: provider slug %q", ErrConflict, profile.Slug)
		}
	}
	return profile, nil
}
