package tests_test

import (
	"context"

	"github.com/dosco/graphjin/core/v3"
)

func sourceModeIntegrationUserContext() context.Context {
	ctx := context.WithValue(context.Background(), core.UserIDKey, "integration-user")
	ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"account_id": "integration-account", "user_id": "integration-user"})
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"user"})
	return ctx
}

func sourceModeIntegrationAdminContext() context.Context {
	ctx := context.WithValue(context.Background(), core.UserIDKey, "integration-admin")
	ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{"account_id": "integration-admin-account", "user_id": "integration-admin"})
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{"admin"})
	return ctx
}
