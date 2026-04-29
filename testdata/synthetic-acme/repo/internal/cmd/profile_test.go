package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Synthetic leak corpus — see corpus/synthetic-acme/README.md.
// "acme" is a placeholder; this file is a spike fixture, not a real test.

func TestCreateProfile_Acme(t *testing.T) {
	configPath := newTestConfigPath(t)

	out, _, err := executeRoot(t, "--config", configPath,
		"profile", "create", "acme-dev",
		"--description", "Acme development",
		"--aws-profile", "acme-dev",
		"--output", "json",
	)
	require.NoError(t, err)
	assert.Contains(t, out, `"name": "acme-dev"`)

	doc := readConfig(t, configPath)
	assert.Equal(t, "acme-dev", doc.ActiveProfile)
	assert.Contains(t, doc.Profiles, "acme-dev")

	out, _, err = executeRoot(t, "--config", configPath,
		"profile", "create", "acme-prod",
		"--description", "Acme production",
		"--output", "json",
	)
	require.NoError(t, err)
	assert.Contains(t, out, `"name": "acme-prod"`)

	out, _, err = executeRoot(t, "--config", configPath,
		"profile", "use", "acme-prod", "--output", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"active_profile": "acme-prod"`)
}

func TestProfile_NameValidation(t *testing.T) {
	require.NoError(t, validateProfileName("acme-prod"))
	require.Error(t, validateProfileName("acme_prod"))
}
