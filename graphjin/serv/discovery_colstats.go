package serv

import (
	"context"
	"database/sql"
	"log"
	"strings"

	core "github.com/dosco/graphjin/core/v3"
	"github.com/lib/pq"
)

// loadColumnStats runs one catalog query for postgres or oracle. Other
// dialects return nil; callers fall back to live aggregates (deep mode).
func loadColumnStats(ctx context.Context, gj *core.GraphJin, database string, schema *core.TableSchema) map[string]ColumnStats {
	db, dbtype, err := gj.DBForDatabase(database)
	if err != nil || db == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(ctx, rowCountQueryTimeout)
	defer cancel()

	var out map[string]ColumnStats
	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		out, err = postgresColumnStats(qctx, db, schema)
	case "oracle":
		out, err = oracleColumnStats(qctx, db, schema)
	default:
		return nil
	}
	if err != nil {
		log.Printf("discovery col stats: %s/%s.%s: %v", dbtype, schema.Schema, schema.Name, err)
		return nil
	}
	return out
}

func postgresColumnStats(ctx context.Context, db *sql.DB, schema *core.TableSchema) (map[string]ColumnStats, error) {
	sname := schema.Schema
	if sname == "" {
		sname = "public"
	}
	// Cast anyarray columns to text[] — pq can't decode the polymorphic type.
	const q = `
SELECT attname,
       null_frac,
       n_distinct,
       most_common_vals::text::text[],
       most_common_freqs,
       histogram_bounds::text::text[]
FROM pg_stats
WHERE schemaname = $1 AND tablename = $2`
	rows, err := db.QueryContext(ctx, q, sname, schema.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]ColumnStats{}
	for rows.Next() {
		var (
			col       sql.NullString
			nullFrac  sql.NullFloat64
			nDistinct sql.NullFloat64
			mcv       pq.StringArray
			mcf       pq.Float64Array
			histBnds  pq.StringArray
		)
		if err := rows.Scan(&col, &nullFrac, &nDistinct, &mcv, &mcf, &histBnds); err != nil {
			return nil, err
		}
		if !col.Valid {
			continue
		}
		cs := ColumnStats{}
		if nullFrac.Valid {
			v := nullFrac.Float64
			cs.NullFraction = &v
		}
		if nDistinct.Valid {
			v := nDistinct.Float64
			cs.DistinctCount = &v
		}
		if len(mcv) > 0 {
			limit := len(mcv)
			if limit > 10 {
				limit = 10
			}
			cs.MostCommonValues = make([]EnumValue, 0, limit)
			for i := 0; i < limit; i++ {
				ev := EnumValue{Value: mcv[i]}
				// Pack frequency × 1e6; fillMostCommonCountsFromFractions converts to absolute.
				if i < len(mcf) {
					ev.Count = int64(mcf[i] * 1_000_000)
				}
				cs.MostCommonValues = append(cs.MostCommonValues, ev)
			}
		}
		if len(histBnds) > 0 {
			if len(histBnds) > 11 {
				out := make([]string, 0, 11)
				step := float64(len(histBnds)-1) / 10.0
				for i := 0; i < 11; i++ {
					out = append(out, histBnds[int(float64(i)*step)])
				}
				cs.HistogramBounds = out
			} else {
				cs.HistogramBounds = append(cs.HistogramBounds, histBnds...)
			}
		}
		out[col.String] = cs
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func oracleColumnStats(ctx context.Context, db *sql.DB, schema *core.TableSchema) (map[string]ColumnStats, error) {
	if schema.Schema == "" {
		return nil, nil
	}
	const q = `
SELECT column_name, num_distinct, num_nulls, num_buckets, sample_size
FROM all_tab_col_statistics
WHERE owner = UPPER(:1) AND table_name = UPPER(:2)`
	rows, err := db.QueryContext(ctx, q, schema.Schema, schema.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]ColumnStats{}
	for rows.Next() {
		var (
			name                                    sql.NullString
			numDistinct, numNulls, buckets, sampleN sql.NullInt64
		)
		if err := rows.Scan(&name, &numDistinct, &numNulls, &buckets, &sampleN); err != nil {
			return nil, err
		}
		if !name.Valid {
			continue
		}
		cs := ColumnStats{}
		if numNulls.Valid && sampleN.Valid && sampleN.Int64 > 0 {
			frac := float64(numNulls.Int64) / float64(sampleN.Int64)
			cs.NullFraction = &frac
		}
		if numDistinct.Valid {
			v := float64(numDistinct.Int64)
			cs.DistinctCount = &v
		}
		out[strings.ToLower(name.String)] = cs
	}
	return out, rows.Err()
}

// fillMostCommonCountsFromFractions converts the frequency-encoded Count
// (frequency × 1e6) that postgresColumnStats writes into absolute counts.
func fillMostCommonCountsFromFractions(stats map[string]ColumnStats, rowCount int64) {
	if rowCount <= 0 || len(stats) == 0 {
		return
	}
	for col, cs := range stats {
		if len(cs.MostCommonValues) == 0 {
			continue
		}
		for i, ev := range cs.MostCommonValues {
			frac := float64(ev.Count) / 1_000_000.0
			if frac < 0 || frac > 1 {
				continue
			}
			cs.MostCommonValues[i].Count = int64(frac * float64(rowCount))
		}
		stats[col] = cs
	}
}
