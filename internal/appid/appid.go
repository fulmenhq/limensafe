package appid

import (
	"context"

	"github.com/fulmenhq/gofulmen/appidentity"

	appidentityassets "github.com/fulmenhq/limensafe/internal/assets/appidentity"
)

func init() {
	// Register embedded identity. As of gofulmen v0.3.5 the discovery
	// precedence puts embedded identity above CWD ancestor search, so a
	// shipped binary self-identifies correctly regardless of where it
	// is run from (fixes the partner-integration devlead 2026-05-08 symptom
	// where limensafe mis-identified as datawidget when run inside
	// the datawidget tree). FULMEN_APP_IDENTITY_PATH still wins over
	// embedded for testing and operator overrides.
	_ = appidentity.RegisterEmbeddedIdentityYAML(appidentityassets.YAML)
}

func Get(ctx context.Context) (*appidentity.Identity, error) {
	return appidentity.Get(ctx)
}
