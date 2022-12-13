package graph

import (
	"testing"
)

func TestFastPrase(t *testing.T) {
	type args struct {
		gql string
	}
	type ts struct {
		name string
		args args
		want FPInfo
		err  bool
	}
	tests := []ts{
		{
			name: "query",
			args: args{gql: `fragment User on users {  slug  firstName: first_name } 
			
			query { 
				query mutation(id: "query {") 
				{ 
					id } 
					subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "query with name",
			args: args{gql: `query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "fragment first, query with name",
			args: args{gql: `fragment User on users { id name } query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "fragment last, query with name",
			args: args{gql: `query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }fragment User on users { id name }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "mutation",
			args: args{gql: `mutation { query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"mutation", ""},
		},
		{
			name: "subscription",
			args: args{gql: `subscription { query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"subscription", ""},
		},
		{
			name: "default query",
			args: args{gql: ` { query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "default query with comment",
			args: args{gql: `#mutation is good
				query { query mutation(id: "query") { id } subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "failed query with comment",
			args: args{gql: `# query is good query { query mutation(id: "query {{") { id } subscription }`},
			err:  true,
		},
		// tests without space after the op type
		{
			name: "query without space",
			args: args{gql: `query{ query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "query with name, without space",
			args: args{gql: `query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "query with name that includes underscores",
			args: args{gql: `query get_cool_stuff{ query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: FPInfo{"query", "get_cool_stuff"},
		},
		{
			name: "fragment first, query with name, without space",
			args: args{gql: `fragment User on users { id name } query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "fragment last, query with name, without space",
			args: args{gql: `query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }fragment User on users { id name }`},
			want: FPInfo{"query", "getStuff"},
		},
		{
			name: "mutation without space",
			args: args{gql: `mutation{ query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"mutation", ""},
		},
		{
			name: "subscription without space",
			args: args{gql: `subscription{ query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"subscription", ""},
		},
		{
			name: "default query without space",
			args: args{gql: `{ query mutation(id: "query {") { id } subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "default query with comment without space",
			args: args{gql: `# mutation is good
				query{ query mutation(id: "query") { id } subscription }`},
			want: FPInfo{"query", ""},
		},
		{
			name: "failed query with comment, without space",
			args: args{gql: `# query is good query{ query mutation(id: "query {{") { id } subscription }`},
			err:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := FastParse(tt.args.gql)

			if tt.err && err != nil {
				return
			}

			if err != nil {
				t.Error(err)
			}

			if h.Operation != tt.want.Operation {
				t.Errorf("operation = %v, want %v", h.Operation, tt.want.Operation)
			}

			if h.Name != tt.want.Name {
				t.Errorf("name = %s, want %s", h.Name, tt.want.Name)
			}
		})
	}
}
