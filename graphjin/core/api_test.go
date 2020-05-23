package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dosco/super-graph/core/internal/psql"
)

func BenchmarkGraphQL(b *testing.B) {
	ct := context.WithValue(context.Background(), UserIDKey, "1")

	db, _, err := sqlmock.New()
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// mock.ExpectQuery(`^SELECT jsonb_build_object`).WithArgs()
	c := &Config{}
	sg, err := newSuperGraph(c, db, psql.GetTestDBInfo())
	if err != nil {
		b.Fatal(err)
	}

	query := `
    query {
      products {
      id
			name
			user {
				full_name
				phone
				email
			}
			customers {
				id
				email
			}
		}
		users {
      id
			name
    }
	}`

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err = sg.GraphQL(ct, query, nil)
		}
	})

	fmt.Println(err)

	//fmt.Println(mock.ExpectationsWereMet())

}
