package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Synthetic leak corpus — see corpus/synthetic-acme/README.md.

func TestLoadConfig_AcmeProfile(t *testing.T) {
	cfg := &Config{
		Profiles: map[string]Profile{
			"acme-dev": {
				Description: "Acme development",
				Providers: map[string]any{
					"s3":    map[string]any{"bucket": "datawidget-artifacts"},
					"trino": map[string]any{"profile": "acme-dev"},
				},
			},
		},
		ActiveProfile: "acme-dev",
	}

	require.NoError(t, validate(cfg))
}
