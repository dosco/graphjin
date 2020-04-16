package qcode

import "testing"

func TestGetQType(t *testing.T) {
	type args struct {
		gql string
	}
	type ts struct {
		name string
		args args
		want QType
	}
	tests := []ts{
		ts{
			name: "query",
			args: args{gql: "  query {"},
			want: QTQuery,
		},
		ts{
			name: "mutation",
			args: args{gql: "  mutation {"},
			want: QTMutation,
		},
		ts{
			name: "default query",
			args: args{gql: "  {"},
			want: QTQuery,
		},
		ts{
			name: "default query with comment",
			args: args{gql: `# query is good 
			{`},
			want: QTQuery,
		},
		ts{
			name: "failed query with comment",
			args: args{gql: `# query is good query {`},
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetQType(tt.args.gql); got != tt.want {
				t.Errorf("GetQType() = %v, want %v", got, tt.want)
			}
		})
	}
}
