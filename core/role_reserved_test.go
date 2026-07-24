package core

import (
	"context"
	"testing"
)

func TestInitialRequestRoleRejectsUntrustedReservedRole(t *testing.T) {
	gj := &graphjinEngine{conf: &Config{Roles: []Role{{Name: "__reserved_internal"}}}}
	ctx := context.WithValue(context.Background(), UserIDKey, "user_1")
	ctx = context.WithValue(ctx, UserRoleKey, "__reserved_internal")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"__reserved_internal"})

	role, trusted := gj.initialRequestRole(ctx)
	if role != "user" || trusted {
		t.Fatalf("initialRequestRole() = %q, %v; want user, false", role, trusted)
	}
}

func TestInitialRequestRoleAllowsTrustedReservedRole(t *testing.T) {
	type trustedKey struct{}
	gj := &graphjinEngine{
		conf: &Config{Roles: []Role{{Name: "__reserved_internal"}}},
		reservedRoleAuthorizer: func(ctx context.Context, role string) bool {
			return role == "__reserved_internal" && ctx.Value(trustedKey{}) == true
		},
	}
	ctx := context.WithValue(context.Background(), trustedKey{}, true)
	ctx = context.WithValue(ctx, UserIDKey, "user_1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"__reserved_internal"})

	role, trusted := gj.initialRequestRole(ctx)
	if role != "__reserved_internal" || !trusted {
		t.Fatalf("initialRequestRole() = %q, %v; want reserved role, true", role, trusted)
	}
}

func TestTrustedReservedSubscriptionBypassesProductionAllowList(t *testing.T) {
	type trustedKey struct{}
	gj := &graphjinEngine{
		conf:    &Config{Roles: []Role{{Name: "__reserved_internal"}}},
		prodSec: true,
		reservedRoleAuthorizer: func(ctx context.Context, role string) bool {
			return role == "__reserved_internal" && ctx.Value(trustedKey{}) == true
		},
	}
	trusted := context.WithValue(context.Background(), trustedKey{}, true)
	trusted = context.WithValue(trusted, UserRoleKey, "__reserved_internal")
	if gj.subscriptionRequiresAllowList(trusted) {
		t.Fatal("trusted reserved subscription should bypass the production allow list")
	}

	forged := context.WithValue(context.Background(), UserRoleKey, "__reserved_internal")
	if !gj.subscriptionRequiresAllowList(forged) {
		t.Fatal("untrusted reserved subscription should require the production allow list")
	}
}
