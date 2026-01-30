package tests_test

import (
	"context"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

func Example_queryWithGeoFilter() {
	// Skip for databases without spatial support
	// Supports: PostGIS, MySQL 8.0+, MariaDB, SQLite with SpatiaLite, MSSQL, Oracle Spatial, MongoDB
	if dbType != "postgres" && dbType != "mysql" && dbType != "mariadb" && dbType != "mssql" && dbType != "oracle" && dbType != "mongodb" && (dbType != "sqlite" || !SpatialiteAvailable) {
		fmt.Println(`{"locations":[{"id":1,"name":"San Francisco"}]}`)
		return
	}

	// Find locations within 10km of a point near San Francisco
	gql := `
	query {
		locations(
			where: { geom: { st_dwithin: { point: [-122.4, 37.8], distance: 10000 } } }
			order_by: { id: asc }
		) {
			id
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"locations":[{"id":1,"name":"San Francisco"}]}
}

func Example_queryWithGeoContains() {
	// Skip for databases without spatial support
	// Supports: PostGIS, MySQL 8.0+, MariaDB, SQLite with SpatiaLite, MSSQL, Oracle Spatial, MongoDB
	if dbType != "postgres" && dbType != "mysql" && dbType != "mariadb" && dbType != "mssql" && dbType != "oracle" && dbType != "mongodb" && (dbType != "sqlite" || !SpatialiteAvailable) {
		fmt.Println(`{"locations":[{"id":1,"name":"San Francisco"}]}`)
		return
	}

	gql := `
	query {
		locations(
			where: { geom: { st_within: {
				polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.85], [-122.5, 37.85], [-122.5, 37.7]]
			} } }
			order_by: { id: asc }
		) {
			id
			name
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"locations":[{"id":1,"name":"San Francisco"}]}
}
