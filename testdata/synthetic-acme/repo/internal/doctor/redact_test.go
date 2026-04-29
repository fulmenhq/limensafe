package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Synthetic leak corpus — see corpus/synthetic-acme/README.md.
// The "acme-horizon-dev" identifier is the worst-case triangulation:
// a single token combining a protected client name with an internal
// project codename. Either token alone is a warning; the combination
// is critical regardless of repo visibility scope.

func TestRedact_AcmeHorizonProfile(t *testing.T) {
	payload := map[string]any{
		"selected_profile": "acme-horizon-dev",
		"redactions":       []string{"client_id", "tenant_label"},
	}

	out := redactPayload(payload)
	assert.NotEqual(t, "acme-horizon-dev", out["selected_profile"],
		"protected triangulation token should be redacted")
}

func TestRedact_BareAcmeProfile(t *testing.T) {
	payload := map[string]any{
		"selected_profile": "acme-dev",
		"providers": map[string]any{
			"aws": map[string]any{"profile": "acme-dev"},
		},
	}
	out := redactPayload(payload)
	assert.NotEqual(t, "acme-dev", out["selected_profile"])
}
