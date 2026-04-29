package auth

import "testing"

func TestCategoryForPath_LongestPrefixWins(t *testing.T) {
	rules := []RouteRule{
		{Prefix: "/", Category: RouteCategoryProtected},
		{Prefix: "/health", Category: RouteCategoryPublic},
		{Prefix: "/health/live", Category: RouteCategoryConditional},
	}

	cat := CategoryForPath("/health/live", rules)
	if cat != RouteCategoryConditional {
		t.Fatalf("got %q, want %q", cat, RouteCategoryConditional)
	}
}

func TestCategoryForPath_DefaultProtected(t *testing.T) {
	cat := CategoryForPath("/anything", nil)
	if cat != RouteCategoryProtected {
		t.Fatalf("got %q, want %q", cat, RouteCategoryProtected)
	}
}
