package cmd

import (
	"testing"

	"github.com/fulmenhq/limensafe/pkg/catalog"
)

func TestClassifyForgeVisibility(t *testing.T) {
	cases := []struct {
		in   string
		want catalog.RepoVisibility
	}{
		{"public", catalog.RepoPublic},
		{"PUBLIC", catalog.RepoPublic},
		{" Public ", catalog.RepoPublic},
		{"private", catalog.RepoNotPublic},
		{"PRIVATE", catalog.RepoNotPublic},
		{"internal", catalog.RepoNotPublic},
		{"", catalog.RepoUnresolved},
		{"unknown-future-value", catalog.RepoUnresolved},
	}
	for _, c := range cases {
		if got := classifyForgeVisibility(c.in); got != c.want {
			t.Errorf("classifyForgeVisibility(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
