package appid

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/fulmenhq/gofulmen/appidentity"
	"gopkg.in/yaml.v3"

	appidentityassets "github.com/fulmenhq/limensafe/internal/assets/appidentity"
)

func init() {
	// Best-effort registration with gofulmen. Kept so any caller that reaches
	// gofulmen's discovery (e.g., a downstream library inside another module)
	// still sees the embedded identity as a fallback. Self-identification in
	// limensafe goes through the local Get below, which short-circuits the
	// buggy precedence — see Get's comment for the gate.
	_ = appidentity.RegisterEmbeddedIdentityYAML(appidentityassets.YAML)
}

// Get returns the application identity for self-identification.
//
// === TEMPORARY workaround — GATED-TO-UNDO before v0.0.3 release ===
//
// gofulmen v0.3.3/v0.3.4 places CWD ancestor search above the registered
// embedded identity in `discoverIdentity`. When limensafe runs from inside
// another workhorse's tree (e.g., datawidget's), the foreign
// `.fulmen/app.yaml` is found first and the binary mis-identifies. Reproduced
// via partner-integration live-validation by devlead 2026-05-08; regression
// test in this package's TestGet_EmbeddedIdentityWinsOverForeignCWD.
//
// This wrapper short-circuits self-identification to the embedded identity
// when no explicit override is set, while still honoring the env-var override
// (FULMEN_APP_IDENTITY_PATH) so test isolation and operator overrides remain
// authoritative.
//
// Coordination memo:
//
//	~/dev/gofulmen/internal coordination notes/limensafe/2026-05-08-appidentity-precedence-bug.md
//
// REMOVAL GATE for v0.0.3 final tag (see RELEASE_CHECKLIST.md):
//  1. gofulmen v0.3.5+ ships discovery-precedence reorder (embedded > CWD)
//  2. Bump go.mod to that version
//  3. Replace this Get body with: return appidentity.Get(ctx)
//  4. Delete embeddedSelfIdentity, the sync.Once state, and embeddedIdentityFile
//  5. Verify TestGet_EmbeddedIdentityWinsOverForeignCWD still passes
func Get(ctx context.Context) (*appidentity.Identity, error) {
	// Honor explicit env-var override — defer to gofulmen for full resolution
	// (which keeps env-var-points-at-missing-file → NotFoundError semantics).
	if os.Getenv(appidentity.EnvIdentityPath) != "" {
		return appidentity.Get(ctx)
	}

	// No override set: embedded identity is authoritative for self-id.
	if id, err := embeddedSelfIdentity(); err == nil && id != nil {
		return id, nil
	}

	// Fallback (only reached if embedded parsing/validation failed at startup,
	// which would also have failed RegisterEmbeddedIdentityYAML in init()).
	return appidentity.Get(ctx)
}

// embeddedIdentityFile mirrors gofulmen's unexported identityFile so the
// embedded YAML can be parsed locally without depending on gofulmen's
// internal loader. Removed alongside Get's workaround.
type embeddedIdentityFile struct {
	App      appidentity.Identity `yaml:"app"`
	Metadata appidentity.Metadata `yaml:"metadata"`
}

var (
	selfIdentityOnce sync.Once
	selfIdentity     *appidentity.Identity
	selfIdentityErr  error
)

// embeddedSelfIdentity parses and validates the embedded identity YAML once,
// then returns the cached *Identity for self-identification. Removed alongside
// Get's workaround.
func embeddedSelfIdentity() (*appidentity.Identity, error) {
	selfIdentityOnce.Do(func() {
		var f embeddedIdentityFile
		if err := yaml.Unmarshal(appidentityassets.YAML, &f); err != nil {
			selfIdentityErr = fmt.Errorf("parse embedded app identity: %w", err)
			return
		}
		f.App.Metadata = f.Metadata
		if err := appidentity.ValidateIdentity(context.Background(), &f.App); err != nil {
			selfIdentityErr = fmt.Errorf("validate embedded app identity: %w", err)
			return
		}
		selfIdentity = &f.App
	})
	return selfIdentity, selfIdentityErr
}
