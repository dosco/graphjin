package tests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompositePKLookup tests that the id: argument works with composite
// primary keys using object syntax: id: {product_id: 1, variant_id: 2}
func TestCompositePKLookup(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite PK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: product_variant with product_id=1, variant_id=2 is "Medium" / "PROD1-M"
	var gtName, gtSku string
	err = db.QueryRow(`SELECT variant_name, sku FROM product_variants WHERE product_id = 1 AND variant_id = 2`).Scan(&gtName, &gtSku)
	require.NoError(t, err, "ground truth query failed")
	require.Equal(t, "Medium", gtName)

	res, err := gj.GraphQL(context.Background(),
		`query {
			product_variants(id: {product_id: 1, variant_id: 2}) {
				product_id
				variant_id
				variant_name
				sku
			}
		}`, nil, nil)
	require.NoError(t, err, "GraphQL composite PK lookup failed")

	var result struct {
		ProductVariants struct {
			ProductID   int    `json:"product_id"`
			VariantID   int    `json:"variant_id"`
			VariantName string `json:"variant_name"`
			Sku         string `json:"sku"`
		} `json:"product_variants"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))

	pv := result.ProductVariants
	assert.Equal(t, 1, pv.ProductID, "product_id mismatch")
	assert.Equal(t, 2, pv.VariantID, "variant_id mismatch")
	assert.Equal(t, gtName, pv.VariantName, "variant_name should match ground truth")
	assert.Equal(t, gtSku, pv.Sku, "sku should match ground truth")
}

// TestCompositePKOrderBy verifies that queries on composite PK tables
// correctly order by all PK columns.
func TestCompositePKOrderBy(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite PK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: count all product_variants
	var gtCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM product_variants`).Scan(&gtCount)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(),
		`query {
			product_variants {
				product_id
				variant_id
				variant_name
				sku
			}
		}`, nil, nil)
	require.NoError(t, err, "GraphQL list query failed")

	var result struct {
		ProductVariants []struct {
			ProductID   int    `json:"product_id"`
			VariantID   int    `json:"variant_id"`
			VariantName string `json:"variant_name"`
			Sku         string `json:"sku"`
		} `json:"product_variants"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	assert.Equal(t, gtCount, len(result.ProductVariants), "should return all product variants")
}

// TestCompositePKFilterWhere tests filtering on composite PK tables
// using the where argument with both PK columns.
func TestCompositePKFilterWhere(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite PK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: all variants for product_id=1
	var gtCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM product_variants WHERE product_id = 1`).Scan(&gtCount)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(),
		`query {
			product_variants(where: {product_id: {eq: 1}}) {
				product_id
				variant_id
				variant_name
			}
		}`, nil, nil)
	require.NoError(t, err, "GraphQL where filter failed")

	var result struct {
		ProductVariants []struct {
			ProductID   int    `json:"product_id"`
			VariantID   int    `json:"variant_id"`
			VariantName string `json:"variant_name"`
		} `json:"product_variants"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	assert.Equal(t, gtCount, len(result.ProductVariants), "should return correct count for product_id=1")

	for _, pv := range result.ProductVariants {
		assert.Equal(t, 1, pv.ProductID, "all results should have product_id=1")
	}
}
