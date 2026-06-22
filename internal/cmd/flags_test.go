package cmd

import "testing"

// The shared catalog/posture flags must be byte-identical (name, default,
// usage) across every command that registers them via
// registerScanCatalogFlags. This is the regression guard for ADR-0007: if the
// helper ever drifts from a command, or a command re-registers its own copy,
// this fails. scan is the canonical definition.
func TestSharedScanCatalogFlags_ParityAcrossCommands(t *testing.T) {
	shared := []string{
		"catalog", "config-file", "visibility", "mode",
		"workers", "max-file-size", "private-catalog-missing",
	}
	for _, name := range shared {
		sf := scanCmd.Flags().Lookup(name)
		af := auditPublishCmd.Flags().Lookup(name)
		if sf == nil {
			t.Errorf("scan is missing shared flag --%s", name)
			continue
		}
		if af == nil {
			t.Errorf("audit-publish is missing shared flag --%s", name)
			continue
		}
		if sf.DefValue != af.DefValue {
			t.Errorf("--%s default drift: scan=%q audit-publish=%q", name, sf.DefValue, af.DefValue)
		}
		if sf.Usage != af.Usage {
			t.Errorf("--%s usage drift:\n  scan=%q\n  audit-publish=%q", name, sf.Usage, af.Usage)
		}
	}
}
