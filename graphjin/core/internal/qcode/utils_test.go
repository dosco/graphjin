package qcode

import (
	"testing"
)

func TestGetQType(t *testing.T) {
	type args struct {
		gql string
	}
	type want struct {
		op   QType
		name string
	}
	type ts struct {
		name string
		args args
		want want
	}
	tests := []ts{
		ts{
			name: "query",
			args: args{gql: `query { query mutation(id: "query {") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "query with name",
			args: args{gql: `query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "fragment first, query with name",
			args: args{gql: `fragment User on users { id name } query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "fragment last, query with name",
			args: args{gql: `query getStuff { query mutation(id: "query \"test1 '{") { id } subscription }fragment User on users { id name }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "mutation",
			args: args{gql: `mutation { query mutation(id: "query {") { id } subscription }`},
			want: want{QTMutation, ""},
		},
		ts{
			name: "subscription",
			args: args{gql: `subscription { query mutation(id: "query {") { id } subscription }`},
			want: want{QTSubscription, ""},
		},
		ts{
			name: "default query",
			args: args{gql: ` { query mutation(id: "query {") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "default query with comment",
			args: args{gql: `# mutation is good
				query { query mutation(id: "query") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "failed query with comment",
			args: args{gql: `# query is good query { query mutation(id: "query {{") { id } subscription }`},
			want: want{QTUnknown, ""},
		},
		// tests without space after the op type
		ts{
			name: "query without space",
			args: args{gql: `query{ query mutation(id: "query {") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "query with name, without space",
			args: args{gql: `query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "fragment first, query with name, without space",
			args: args{gql: `fragment User on users { id name } query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "fragment last, query with name, without space",
			args: args{gql: `query getStuff{ query mutation(id: "query \"test1 '{") { id } subscription }fragment User on users { id name }`},
			want: want{QTQuery, "getStuff"},
		},
		ts{
			name: "mutation without space",
			args: args{gql: `mutation{ query mutation(id: "query {") { id } subscription }`},
			want: want{QTMutation, ""},
		},
		ts{
			name: "subscription without space",
			args: args{gql: `subscription{ query mutation(id: "query {") { id } subscription }`},
			want: want{QTSubscription, ""},
		},
		ts{
			name: "default query without space",
			args: args{gql: `{ query mutation(id: "query {") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "default query with comment without space",
			args: args{gql: `# mutation is good
				query{ query mutation(id: "query") { id } subscription }`},
			want: want{QTQuery, ""},
		},
		ts{
			name: "failed query with comment, without space",
			args: args{gql: `# query is good query{ query mutation(id: "query {{") { id } subscription }`},
			want: want{QTUnknown, ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, name := GetQType(tt.args.gql)

			if op != tt.want.op {
				t.Errorf("operation = %v, want %v", op, tt.want.op)
			}

			if name != tt.want.name {
				t.Errorf("name = %s, want %s", name, tt.want.name)
			}
		})
	}
}
