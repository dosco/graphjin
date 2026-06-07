package dialect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// RedshiftDialect is intentionally Redshift-owned even where it reuses the
// warehouse-oriented Snowflake path. Redshift is not plain Postgres; every
// inherited behavior should stay covered by Redshift-specific tests.
type RedshiftDialect struct {
	SnowflakeDialect
}

var _ Dialect = (*RedshiftDialect)(nil)

func (d *RedshiftDialect) Name() string {
	return "redshift"
}

func (d *RedshiftDialect) SupportsReturning() bool {
	return false
}

func (d *RedshiftDialect) SupportsWritableCTE() bool {
	return false
}

func (d *RedshiftDialect) SupportsConflictUpdate() bool {
	return false
}

func (d *RedshiftDialect) SupportsSubscriptionBatching() bool {
	return true
}

func (d *RedshiftDialect) RenderSubscriptionUnbox(ctx Context, params []Param, innerSQL string) {
	ctx.WriteString(`WITH _gj_sub AS (SELECT `)
	for i, p := range params {
		if i != 0 {
			ctx.WriteString(`, `)
		}
		ctx.WriteString(`CAST(_gj_sub_unboxed.value[`)
		ctx.WriteString(fmt.Sprintf("%d", i))
		ctx.WriteString(`] AS `)
		ctx.WriteString(redshiftSubscriptionParamType(p.Type))
		ctx.WriteString(`) AS `)
		ctx.Quote(p.Name)
	}
	if len(params) != 0 {
		ctx.WriteString(`, `)
	}
	ctx.WriteString(`_gj_sub_unboxed.idx AS "__gj_sub_order"`)
	ctx.WriteString(` FROM (SELECT JSON_PARSE(?) AS _gj_params) AS _gj_sub_input, UNNEST(_gj_sub_input._gj_params) WITH OFFSET AS _gj_sub_unboxed(value, idx))`)
	ctx.WriteString(` SELECT (`)
	ctx.WriteString(innerSQL)
	ctx.WriteString(`) AS "__root" FROM _gj_sub ORDER BY "__gj_sub_order"`)
}

func redshiftSubscriptionParamType(typ string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "text", "varchar", "character varying", "char", "character", "cursor":
		return "VARCHAR"
	case "int", "int4", "integer":
		return "INTEGER"
	case "int8", "bigint":
		return "BIGINT"
	case "float", "float4", "real":
		return "REAL"
	case "float8", "double", "double precision":
		return "DOUBLE PRECISION"
	case "numeric", "decimal", "number":
		return "DECIMAL"
	case "bool", "boolean":
		return "BOOLEAN"
	case "json", "jsonb", "super":
		return "SUPER"
	default:
		return typ
	}
}

func (d *RedshiftDialect) RenderSetup(ctx Context) {
	ctx.WriteString(`DROP TABLE IF EXISTS `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(`; DROP TABLE IF EXISTS `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(`; `)
	ctx.WriteString(`CREATE TEMP TABLE `)
	ctx.WriteString(d.idsTableName(ctx))
	ctx.WriteString(` (k VARCHAR, id VARCHAR); `)
	ctx.WriteString(`CREATE TEMP TABLE `)
	ctx.WriteString(d.prevIDsTableName(ctx))
	ctx.WriteString(` (k VARCHAR, id VARCHAR); `)
}

func (d *RedshiftDialect) RenderGeoOp(ctx Context, table, col string, ex *qcode.Exp) error {
	geo := ex.Geo
	if geo == nil {
		return fmt.Errorf("missing GIS expression")
	}

	switch ex.Op {
	case qcode.OpGeoDistance, qcode.OpGeoNear:
		ctx.WriteString(`ST_DWithin(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`, `)
		d.renderRedshiftGeoGeometry(ctx, geo)
		ctx.WriteString(`, `)
		d.renderRedshiftGeoDistance(ctx, geo)
		ctx.WriteString(`)`)

	case qcode.OpGeoWithin:
		ctx.WriteString(`ST_Within(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`, `)
		d.renderRedshiftGeoGeometry(ctx, geo)
		ctx.WriteString(`)`)

	case qcode.OpGeoContains:
		ctx.WriteString(`ST_Contains(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`, `)
		d.renderRedshiftGeoGeometry(ctx, geo)
		ctx.WriteString(`)`)

	case qcode.OpGeoIntersects:
		ctx.WriteString(`ST_Intersects(`)
		ctx.ColWithTable(table, col)
		ctx.WriteString(`, `)
		d.renderRedshiftGeoGeometry(ctx, geo)
		ctx.WriteString(`)`)

	default:
		return fmt.Errorf("unsupported Redshift GIS operator: %v", ex.Op)
	}

	return nil
}

func (d *RedshiftDialect) renderRedshiftGeoGeometry(ctx Context, geo *qcode.GeoExp) {
	if len(geo.Point) == 2 {
		ctx.WriteString(fmt.Sprintf(`ST_GeomFromText('POINT(%f %f)', %d)`,
			geo.Point[0], geo.Point[1], geo.SRID))
		return
	}

	if len(geo.Polygon) > 0 {
		ctx.WriteString(`ST_GeomFromText('POLYGON((`)
		for i, pt := range geo.Polygon {
			if i != 0 {
				ctx.WriteString(`, `)
			}
			ctx.WriteString(fmt.Sprintf(`%f %f`, pt[0], pt[1]))
		}
		ctx.WriteString(fmt.Sprintf(`))', %d)`, geo.SRID))
		return
	}

	if len(geo.GeoJSON) == 0 {
		return
	}

	if bytes.Contains(geo.GeoJSON, []byte(`"$var"`)) {
		var varRef struct {
			Var string `json:"$var"`
		}
		if err := json.Unmarshal(geo.GeoJSON, &varRef); err == nil && varRef.Var != "" {
			ctx.WriteString(`ST_GeomFromGeoJSON(`)
			ctx.AddParam(Param{Name: varRef.Var, Type: "json"})
			ctx.WriteString(`)`)
			return
		}
	}

	ctx.WriteString(fmt.Sprintf(`ST_GeomFromGeoJSON('%s')`,
		strings.ReplaceAll(string(geo.GeoJSON), "'", "''")))
}

func (d *RedshiftDialect) renderRedshiftGeoDistance(ctx Context, geo *qcode.GeoExp) {
	if geo.DistanceVar != "" {
		ctx.AddParam(Param{Name: geo.DistanceVar, Type: "float8"})
		if geo.Unit != qcode.GeoUnitMeters {
			ctx.WriteString(fmt.Sprintf(` * %f`, geo.Unit.ToMeters(1)))
		}
		return
	}

	ctx.WriteString(fmt.Sprintf(`%f`, geo.Unit.ToMeters(geo.Distance)))
}
