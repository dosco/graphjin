package tests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompositeFKJoinOrderItemToVariant tests that a composite FK
// (product_id, variant_id) on order_items correctly joins to the
// matching product_variants row. Without composite FK support, the join
// would only use one column and return the wrong variant.
func TestCompositeFKJoinOrderItemToVariant(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite FK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: order_item with product_id=1, variant_id=2 should
	// join to product_variant "Medium" (PROD1-M), not "Small" (PROD1-S)
	var gtVariantName, gtSku string
	err = db.QueryRow(`
		SELECT pv.variant_name, pv.sku
		FROM order_items oi
		JOIN product_variants pv ON oi.product_id = pv.product_id AND oi.variant_id = pv.variant_id
		WHERE oi.product_id = 1 AND oi.variant_id = 2`).Scan(&gtVariantName, &gtSku)
	require.NoError(t, err, "ground truth query failed")
	require.Equal(t, "Medium", gtVariantName)
	require.Equal(t, "PROD1-M", gtSku)
	t.Logf("Ground truth: variant=%s sku=%s", gtVariantName, gtSku)

	res, err := gj.GraphQL(context.Background(),
		`query {
			order_items(where: {product_id: {eq: 1}, variant_id: {eq: 2}}) {
				id
				product_id
				variant_id
				quantity
				price
				product_variants {
					product_id
					variant_id
					variant_name
					sku
				}
			}
		}`, nil, nil)
	require.NoError(t, err, "GraphQL query failed")

	var result struct {
		OrderItems []struct {
			ID        int     `json:"id"`
			ProductID int     `json:"product_id"`
			VariantID int     `json:"variant_id"`
			Quantity  int     `json:"quantity"`
			Price     float64 `json:"price"`
			Variant   struct {
				ProductID   int    `json:"product_id"`
				VariantID   int    `json:"variant_id"`
				VariantName string `json:"variant_name"`
				Sku         string `json:"sku"`
			} `json:"product_variants"`
		} `json:"order_items"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.NotEmpty(t, result.OrderItems, "expected order items")

	oi := result.OrderItems[0]
	assert.Equal(t, 1, oi.ProductID)
	assert.Equal(t, 2, oi.VariantID)

	// Critical assertion: the composite FK join must return the correct variant
	assert.Equal(t, 1, oi.Variant.ProductID, "variant product_id should match")
	assert.Equal(t, 2, oi.Variant.VariantID, "variant variant_id should match")
	assert.Equal(t, "Medium", oi.Variant.VariantName, "should be Medium variant, not Small")
	assert.Equal(t, "PROD1-M", oi.Variant.Sku, "should be PROD1-M sku")
}

// TestCompositeFKAllRowsMatch verifies that every order_item joins to the
// correct product_variant — not just a single row. This catches bugs where
// only one column of the composite FK is used in the join condition.
func TestCompositeFKAllRowsMatch(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite FK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: all 5 order items with their correct variant names
	type groundTruth struct {
		ProductID   int
		VariantID   int
		VariantName string
	}
	rows, err := db.Query(`
		SELECT oi.product_id, oi.variant_id, pv.variant_name
		FROM order_items oi
		JOIN product_variants pv ON oi.product_id = pv.product_id AND oi.variant_id = pv.variant_id
		ORDER BY oi.id`)
	require.NoError(t, err)
	defer rows.Close()

	var expected []groundTruth
	for rows.Next() {
		var gt groundTruth
		require.NoError(t, rows.Scan(&gt.ProductID, &gt.VariantID, &gt.VariantName))
		expected = append(expected, gt)
	}
	require.Len(t, expected, 5, "should have 5 order items")
	t.Logf("Ground truth: %+v", expected)

	res, err := gj.GraphQL(context.Background(),
		`query {
			order_items(order_by: {id: asc}) {
				product_id
				variant_id
				product_variants {
					variant_name
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		OrderItems []struct {
			ProductID int `json:"product_id"`
			VariantID int `json:"variant_id"`
			Variant   struct {
				VariantName string `json:"variant_name"`
			} `json:"product_variants"`
		} `json:"order_items"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.OrderItems, 5, "should return all 5 order items")

	for i, oi := range result.OrderItems {
		assert.Equal(t, expected[i].ProductID, oi.ProductID, "product_id mismatch at row %d", i)
		assert.Equal(t, expected[i].VariantID, oi.VariantID, "variant_id mismatch at row %d", i)
		assert.Equal(t, expected[i].VariantName, oi.Variant.VariantName,
			"variant_name mismatch at row %d: got %q, want %q (product_id=%d, variant_id=%d)",
			i, oi.Variant.VariantName, expected[i].VariantName, oi.ProductID, oi.VariantID)
	}
}

// TestCompositeFKReverseJoin tests the reverse direction: querying
// product_variants and getting their associated order_items.
func TestCompositeFKReverseJoin(t *testing.T) {
	if dbType == "mongodb" || dbType == "snowflake" || dbType == "adventureworks" {
		t.Skipf("skipping composite FK test for %s", dbType)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	defer gj.Close()

	// Ground truth: product_variant (1,2) "Medium" has exactly 1 order_item
	var orderItemCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM order_items
		WHERE product_id = 1 AND variant_id = 2`).Scan(&orderItemCount)
	require.NoError(t, err)
	require.Equal(t, 1, orderItemCount)

	res, err := gj.GraphQL(context.Background(),
		`query {
			product_variants(where: {product_id: {eq: 1}, variant_id: {eq: 2}}) {
				product_id
				variant_id
				variant_name
				order_items {
					id
					quantity
					price
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Variants []struct {
			ProductID   int    `json:"product_id"`
			VariantID   int    `json:"variant_id"`
			VariantName string `json:"variant_name"`
			OrderItems  []struct {
				ID       int     `json:"id"`
				Quantity int     `json:"quantity"`
				Price    float64 `json:"price"`
			} `json:"order_items"`
		} `json:"product_variants"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.NotEmpty(t, result.Variants)

	v := result.Variants[0]
	assert.Equal(t, "Medium", v.VariantName)
	assert.Len(t, v.OrderItems, 1, "Medium variant should have exactly 1 order item")
	assert.Equal(t, 1, v.OrderItems[0].Quantity)
	assert.InDelta(t, 24.99, v.OrderItems[0].Price, 0.01)
}
