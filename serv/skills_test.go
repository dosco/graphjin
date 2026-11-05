package serv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

// skillGraphQL runs one GraphQL request as the caller, the way a skills GUI
// would, and returns the gj_artifacts payload or the error text.
func skillGraphQL(t *testing.T, svc *graphjinService, ctx context.Context, query string, vars map[string]any) (json.RawMessage, string) {
	t.Helper()
	raw, _ := json.Marshal(vars)
	res, err := svc.gj.GraphQL(svc.applyIdentityContext(ctx), query, raw, &core.RequestConfig{})
	if err != nil {
		return nil, err.Error()
	}
	if len(res.Errors) != 0 {
		return nil, catalogGraphQLErrors(res.Errors)
	}
	var out struct {
		Rows json.RawMessage `json:"gj_artifacts"`
	}
	_ = json.Unmarshal(res.Data, &out)
	return out.Rows, ""
}

const insertSkillQuery = `mutation add_skill($name: String!, $description: String!, $content: String!) {
	gj_artifacts(insert: { kind: "skill", name: $name, description: $description, content: $content }) { id name tier description }
}`

// Skills are plain gj_artifacts rows: created, listed, edited, shared and
// deleted through GraphQL, scoped like memories.
func TestSkillsAreGraphQLArtifacts(t *testing.T) {
	svc := newSQLiteThreadService(t)
	alice := profileUserCtx("alice", "acct_1")
	bob := profileUserCtx("bob", "acct_1")
	carol := profileUserCtx("carol", "acct_2")

	raw, errText := skillGraphQL(t, svc, alice, insertSkillQuery, map[string]any{
		"name": "churn review", "description": "How we report churn", "content": "Report churn as a count and the MRR lost, last 30 days.",
	})
	if errText != "" {
		t.Fatalf("insert skill: %s", errText)
	}
	var created map[string]any
	if err := json.Unmarshal(raw, &created); err != nil || created["id"] == nil {
		t.Fatalf("insert returned %s", raw)
	}
	id := created["id"].(string)
	if stringMapValue(created, "description") != "How we report churn" {
		t.Fatalf("description not stored: %+v", created)
	}

	if _, errText := skillGraphQL(t, svc, alice, insertSkillQuery, map[string]any{
		"name": "Churn Review", "description": "dup", "content": "dup",
	}); !strings.Contains(errText, "already exists") {
		t.Fatalf("duplicate name = %q, want refused", errText)
	}
	if _, errText := skillGraphQL(t, svc, alice, insertSkillQuery, map[string]any{
		"name": "long", "description": strings.Repeat("x", maxSkillDescriptionBytes+1), "content": "c",
	}); !strings.Contains(errText, "description exceeds") {
		t.Fatalf("long description = %q, want refused", errText)
	}

	store := graphLeadStore{s: svc}
	sees := func(ctx context.Context) int {
		t.Helper()
		skills, err := store.Skills(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(skills)
	}
	if sees(alice) != 1 || sees(bob) != 0 || sees(carol) != 0 {
		t.Fatalf("personal skill visibility: alice=%d bob=%d carol=%d", sees(alice), sees(bob), sees(carol))
	}

	if _, errText := skillGraphQL(t, svc, bob, `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, update: { content: "hijack" }) { id } }`, nil); errText == "" {
		t.Fatal("a teammate edited alice's skill")
	}
	if _, errText := skillGraphQL(t, svc, alice, `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, update: { tier: "approved" }) { id tier } }`, nil); errText != "" {
		t.Fatalf("share skill: %s", errText)
	}
	if sees(alice) != 1 || sees(bob) != 1 || sees(carol) != 0 {
		t.Fatalf("shared skill visibility: alice=%d bob=%d carol=%d", sees(alice), sees(bob), sees(carol))
	}
	skills, _ := store.Skills(bob)
	if skills[0].Description != "How we report churn" || !strings.Contains(skills[0].Content, "MRR lost") {
		t.Fatalf("teammate's view of the skill = %+v", skills[0])
	}

	if _, errText := skillGraphQL(t, svc, alice, `mutation { gj_artifacts(where: { id: { eq: "`+id+`" } }, delete: true) { id } }`, nil); errText != "" {
		t.Fatalf("delete skill: %s", errText)
	}
	if sees(alice) != 0 || sees(bob) != 0 {
		t.Fatal("a deleted skill is still visible")
	}
}
