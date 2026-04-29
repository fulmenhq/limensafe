package auth

import "strings"

type RouteCategory string

const (
	RouteCategoryDeny        RouteCategory = "deny"
	RouteCategoryPublic      RouteCategory = "public"
	RouteCategoryConditional RouteCategory = "conditional"
	RouteCategoryProtected   RouteCategory = "protected"
)

type RouteRule struct {
	Prefix   string
	Category RouteCategory
}

// CategoryForPath returns the effective route category using longest-prefix match.
// If no rules match, it returns RouteCategoryProtected.
func CategoryForPath(path string, rules []RouteRule) RouteCategory {
	bestLen := -1
	best := RouteCategoryProtected

	for _, rule := range rules {
		pfx := rule.Prefix
		if pfx == "" {
			continue
		}
		if strings.HasPrefix(path, pfx) {
			if l := len(pfx); l > bestLen {
				bestLen = l
				best = rule.Category
			}
		}
	}

	return best
}
