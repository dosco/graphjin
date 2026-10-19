package core

import (
	"context"
	"testing"
)

func TestAllowListLearningFollowsDeploymentMode(t *testing.T) {
	for _, test := range []struct {
		mode       string
		wantLearns int
	}{
		{mode: "dev", wantLearns: 2},
		{mode: "agentic", wantLearns: 0},
	} {
		t.Run(test.mode, func(t *testing.T) {
			db, err := NewNanoDB(NanoSnapshot{
				Schema: "main",
				Tables: []NanoTable{{
					Name:    "records",
					Columns: []NanoColumn{{Name: "id", Type: "integer", PrimaryKey: true}},
					Rows:    []NanoRow{{"id": 1}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var learned []SavedQuerySaveRequest
			gj, err := NewGraphJin(&Config{
				Mode:      test.mode,
				DBType:    "nanodb",
				Databases: map[string]DatabaseConfig{DefaultDBName: {Type: "nanodb"}},
			}, nil,
				OptionSetNanoDatabases(map[string]*NanoDB{DefaultDBName: db}),
				OptionSetSavedQuerySaveHook(func(_ context.Context, req SavedQuerySaveRequest) (bool, error) {
					learned = append(learned, req)
					return true, nil
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer gj.Close()

			result, err := gj.GraphQL(context.Background(), `query learned_query { records { id } }`, nil, nil)
			if err != nil || len(result.Errors) != 0 {
				t.Fatalf("named query: err=%v errors=%+v", err, result.Errors)
			}
			member, err := gj.Subscribe(context.Background(), `subscription learned_subscription { records { id } }`, nil, nil)
			if err != nil {
				t.Fatalf("named subscription: %v", err)
			}
			member.Unsubscribe()

			if len(learned) != test.wantLearns {
				t.Fatalf("learned entries = %d, want %d: %+v", len(learned), test.wantLearns, learned)
			}
			if test.wantLearns == 2 && (learned[0].Operation != "query" || learned[1].Operation != "subscription") {
				t.Fatalf("learned operations = %q, %q", learned[0].Operation, learned[1].Operation)
			}
		})
	}
}
