package serv

import (
	"context"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestCallerAllowedActionsFollowSourceAccessAndIdentity(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true})
	source := &ms.service.conf.Core.Sources[0]
	source.Capabilities = map[string]bool{sourcecap.KeyDataWrite: true}
	source.Access = core.SourceAccessConfig{
		Read:   core.AccessModeAccount,
		Write:  core.AccessModeAuthenticated,
		Delete: core.AccessModeBlocked,
	}

	anon := ms.callerAllowedActions(context.Background())
	for _, action := range []string{gjagent.CapabilityActionDataInsert, gjagent.CapabilityActionDataUpdate, gjagent.CapabilityActionDataDelete} {
		if stringSliceContains(anon, action) {
			t.Fatalf("anonymous actions = %+v, unexpectedly granted %s", anon, action)
		}
	}

	user := ms.callerAllowedActions(sourceModeUserTestContext())
	for _, action := range []string{gjagent.CapabilityActionDataInsert, gjagent.CapabilityActionDataUpdate} {
		if !stringSliceContains(user, action) {
			t.Fatalf("user actions = %+v, missing %s", user, action)
		}
	}
	if stringSliceContains(user, gjagent.CapabilityActionDataDelete) {
		t.Fatalf("user actions = %+v, delete must remain blocked", user)
	}

	source.ReadOnly = true
	readOnlySource := ms.callerAllowedActions(sourceModeUserTestContext())
	for _, action := range []string{gjagent.CapabilityActionDataInsert, gjagent.CapabilityActionDataUpdate, gjagent.CapabilityActionDataDelete} {
		if stringSliceContains(readOnlySource, action) {
			t.Fatalf("read-only source actions = %+v, unexpectedly granted %s", readOnlySource, action)
		}
	}
	source.ReadOnly = false
	ms.service.conf.Agent.ReadOnly = true
	if actions := ms.callerAllowedActions(sourceModeUserTestContext()); len(actions) != 0 {
		t.Fatalf("read-only agent actions = %+v, want none", actions)
	}

	noRaw := mockMcpServerWithConfig(MCPConfig{})
	noRaw.service.conf.Core.Sources[0].Access = core.SourceAccessConfig{Write: core.AccessModeAuthenticated}
	if actions := noRaw.callerAllowedActions(sourceModeUserTestContext()); len(actions) != 0 {
		t.Fatalf("profile without execute_graphql actions = %+v, want none", actions)
	}
}

func TestCallerCapabilityProfilePublishesOwnerScopedRootActionsOnlyToAuthenticatedCaller(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{AllowRawQueries: true})
	ms.service.conf.Core.Tasks.Enabled = true

	anon := ms.callerCapabilityProfile(context.Background(), false)
	user := ms.callerCapabilityProfile(sourceModeUserTestContext(), false)
	for _, action := range []string{"gj_artifacts.insert", "gj_watch.insert", "gj_task.insert"} {
		if stringSliceContains(anon.AllowedActions, action) {
			t.Fatalf("anonymous profile actions = %+v, unexpectedly granted %s", anon.AllowedActions, action)
		}
		if !stringSliceContains(user.AllowedActions, action) {
			t.Fatalf("user profile actions = %+v, missing %s", user.AllowedActions, action)
		}
	}
}
