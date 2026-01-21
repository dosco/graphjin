package psql_test

import (
	"encoding/json"
	"testing"
)

func geoStDWithinPoint(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 1000 } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStDWithinWithUnit(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 5, unit: "miles" } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStDWithinVariable(t *testing.T) {
	gql := `query($loc: JSON!, $radius: Float!) {
		locations(where: {
			geom: { st_dwithin: { point: $loc, distance: $radius } }
		}) {
			id
			name
		}
	}`

	vars := map[string]json.RawMessage{
		"loc":    json.RawMessage(`[-122.4194, 37.7749]`),
		"radius": json.RawMessage(`1000`),
	}

	compileGQLToPSQL(t, gql, vars, "user")
}

func geoStContainsPoint(t *testing.T) {
	gql := `query {
		locations(where: {
			boundary: { st_contains: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStWithinPolygon(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_within: {
				polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]
			}}
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStIntersectsGeoJSON(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_intersects: {
				geometry: { type: "Polygon", coordinates: [[[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]] }
			}}
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoNear(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { near: { point: [-122.4194, 37.7749], maxDistance: 5000 } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStTouches(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_touches: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStOverlaps(t *testing.T) {
	gql := `query {
		locations(where: {
			boundary: { st_overlaps: {
				polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]
			}}
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStCoveredBy(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_coveredby: {
				polygon: [[-122.5, 37.7], [-122.3, 37.7], [-122.3, 37.9], [-122.5, 37.9], [-122.5, 37.7]]
			}}
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoStCovers(t *testing.T) {
	gql := `query {
		locations(where: {
			boundary: { st_covers: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func geoCombinedWithOtherFilters(t *testing.T) {
	gql := `query {
		locations(where: {
			and: {
				name: { like: "San%" },
				geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 10000 } }
			}
		}) {
			id
			name
		}
	}`

	compileGQLToPSQL(t, gql, nil, "user")
}

func TestCompileGeoQuery(t *testing.T) {
	t.Run("geoStDWithinPoint", geoStDWithinPoint)
	t.Run("geoStDWithinWithUnit", geoStDWithinWithUnit)
	t.Run("geoStDWithinVariable", geoStDWithinVariable)
	t.Run("geoStContainsPoint", geoStContainsPoint)
	t.Run("geoStWithinPolygon", geoStWithinPolygon)
	t.Run("geoStIntersectsGeoJSON", geoStIntersectsGeoJSON)
	t.Run("geoNear", geoNear)
	t.Run("geoStTouches", geoStTouches)
	t.Run("geoStOverlaps", geoStOverlaps)
	t.Run("geoStCoveredBy", geoStCoveredBy)
	t.Run("geoStCovers", geoStCovers)
	t.Run("geoCombinedWithOtherFilters", geoCombinedWithOtherFilters)
}
