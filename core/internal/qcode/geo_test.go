package qcode_test

import (
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

var geoSchema *sdata.DBSchema

func init() {
	var err error
	geoSchema, err = sdata.NewDBSchema(sdata.GetTestDBInfo(), nil)
	if err != nil {
		panic(err)
	}
}

func TestGeoStDWithinPoint(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 1000 } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStDWithinWithUnit(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 5, unit: "miles" } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStDWithinVariable(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), vars, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStContainsPoint(t *testing.T) {
	gql := `query {
		locations(where: {
			boundary: { st_contains: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStWithinPolygon(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStIntersectsGeoJSON(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoNear(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { near: { point: [-122.4194, 37.7749], maxDistance: 5000 } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStDWithinWithSRID(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_dwithin: { point: [-122.4194, 37.7749], distance: 1000, srid: 4326 } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStTouches(t *testing.T) {
	gql := `query {
		locations(where: {
			geom: { st_touches: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStOverlaps(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStCoveredBy(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoStCovers(t *testing.T) {
	gql := `query {
		locations(where: {
			boundary: { st_covers: { point: [-122.4194, 37.7749] } }
		}) {
			id
			name
		}
	}`

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeoCombinedWithOtherFilters(t *testing.T) {
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

	qc, err := qcode.NewCompiler(geoSchema, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qc.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}
}
