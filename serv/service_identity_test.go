package serv

import (
	"context"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

func newServiceIdentityService(t *testing.T) *graphjinService {
	t.Helper()
	db, svc := newSQLiteWatchServiceWithOptions(t, 10, func(conf *Config) {
		conf.Agent.Enabled = true
		conf.Agent.Lead.Enabled = true
		conf.Agent.ServiceIdentity = gjagent.ServiceIdentityConfig{UserID: "svc_ops", Role: "ops_agent"}
		conf.Core.Roles = append(conf.Core.Roles, core.Role{Name: "ops_agent"})
		conf.Core.Threads = core.ThreadsConfig{Enabled: true, HistoryTurns: 12, RunLeaseSeconds: 3, MaxPerOwner: 5}
	})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	startSQLiteWatchCore(t, svc, db)
	return svc
}

func createOpsAgent(t *testing.T, svc *graphjinService) string {
	t.Helper()
	admin := contextWithUserRole(artifactUserCtx("admin_1"), "admin")
	res, err := svc.gj.GraphQL(admin, `mutation { gj_artifacts(insert: { kind: "agent_profile", name: "Ops", content: "Keep orders tidy.", tier: "approved", runs_as: "service" }) { id } }`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return stringFromAny(mapFromAny(mapFromAny(parseJSONValue(string(res.Data)))["gj_artifacts"])["id"])
}

func TestValidateServiceIdentity(t *testing.T) {
	conf := &Config{}
	conf.Core.Roles = []core.Role{{Name: "ops_agent"}}
	for _, test := range []struct {
		id gjagent.ServiceIdentityConfig
		ok bool
	}{
		{gjagent.ServiceIdentityConfig{}, true},
		{gjagent.ServiceIdentityConfig{UserID: "svc", Role: "ops_agent"}, true},
		{gjagent.ServiceIdentityConfig{UserID: "svc", Role: "admin"}, true},
		{gjagent.ServiceIdentityConfig{UserID: "svc"}, false},
		{gjagent.ServiceIdentityConfig{UserID: "svc", Role: "anon"}, false},
		{gjagent.ServiceIdentityConfig{UserID: "svc", Role: "nobody"}, false},
	} {
		conf.Agent.ServiceIdentity = test.id
		if err := validateServiceIdentity(conf); (err == nil) != test.ok {
			t.Errorf("%+v: err = %v", test.id, err)
		}
	}
}

// Only an admin marks an agent runs_as service, and anyone else editing its
// instructions turns that off.
func TestOnlyAnAdminMakesAnAgentRunAsTheService(t *testing.T) {
	svc := newServiceIdentityService(t)
	user := threadUserCtx("user_1")
	if _, err := svc.gj.GraphQL(user, `mutation { gj_artifacts(insert: { kind: "agent_profile", name: "Mine", content: "x", runs_as: "service" }) { id } }`, nil, nil); err == nil || !strings.Contains(err.Error(), "only an admin") {
		t.Fatalf("a non-admin set runs_as: %v", err)
	}
	id := createOpsAgent(t, svc)
	row, _ := svc.internalArtifactStoreRow(user, id)
	if profileRunsAs(row) != profileRunsAsService {
		t.Fatalf("runs_as not stored: %v", row["metadata_json"])
	}
}

// An admin's routine on a service agent thinks and acts as the service
// identity, in the admin's own thread, recording the delegation. A
// non-admin's routine on the same agent runs as its owner.
func TestServiceAgentRoutineRunsAsTheServiceForAnAdmin(t *testing.T) {
	svc := newServiceIdentityService(t)
	profile := createOpsAgent(t, svc)
	runner := &scriptedAgentRunner{resp: gjagent.Response{Status: gjagent.StatusAnswered, Answer: "Two stale orders."}}
	withScriptedAgentRunner(t, runner)

	admin := contextWithUserRole(artifactUserCtx("admin_1"), "admin")
	fireRoutine(t, svc, admin, map[string]any{"kind": "agent", "prompt": "Check orders.", "profile": profile})
	if got, _ := artifactUserID(runner.ctx); got != "svc_ops" || runtimeRoleClass(runner.ctx) != "ops_agent" {
		t.Fatalf("routine ran as %q / %q", got, runtimeRoleClass(runner.ctx))
	}
	origin := runOriginFrom(runner.ctx)
	if origin.DelegatedBy != "admin_1" || origin.RunAsID != "svc_ops" {
		t.Fatalf("origin = %+v", origin)
	}
	home := profileHomeThreadID("admin_1", profile)
	messages, err := svc.threadMessages(admin, home)
	if err != nil || len(messages) != 2 {
		t.Fatalf("the admin's home thread = %v err %v", messages, err)
	}
	run, _ := svc.runStoreRow(admin, stringFromAny(messages[1]["run_id"]))
	if stringMapValue(run, "delegated_by") != "admin_1" || stringMapValue(run, "run_as_id") != "svc_ops" {
		t.Fatalf("run row = %v", run)
	}

	// The same agent, used by someone who could not have granted its role.
	if _, err := svc.dbs["app"].Exec(`DELETE FROM "_graphjin_watches"`); err != nil {
		t.Fatal(err)
	}
	user := threadUserCtx("user_1")
	fireRoutine(t, svc, user, map[string]any{"kind": "agent", "prompt": "Check orders.", "profile": profile})
	if got, _ := artifactUserID(runner.ctx); got != "user_1" || runOriginFrom(runner.ctx).DelegatedBy != "" {
		t.Fatalf("a non-admin's routine ran as %q (%+v)", got, runOriginFrom(runner.ctx))
	}
}

// A run acting as the service identity schedules no check-backs: the watch
// would be the service's, in a thread owned by whoever delegated the run, and
// could never deliver there.
func TestServiceIdentityRunsScheduleNoFollowUps(t *testing.T) {
	r := leadRunner{followUps: followUpScheduler{}}
	if r.followUpsFor(context.Background()) == nil {
		t.Fatal("an owner's run should be able to schedule a follow-up")
	}
	asService := withRunOrigin(context.Background(), runOrigin{Origin: approvalOriginRoutine, DelegatedBy: "admin_1", RunAsID: "svc_ops", RunAsRole: "ops_agent"})
	if r.followUpsFor(asService) != nil {
		t.Fatal("a service-identity run must not schedule a follow-up")
	}
	if (leadRunner{}).followUpsFor(context.Background()) != nil {
		t.Fatal("no scheduler, no follow-ups")
	}
}
