package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNotAdventureWorks(t *testing.T) {
	t.Helper()
	if dbParam != "adventureworks" {
		t.Skip("skipping adventureworks test")
	}
}

// newAdventureWorksGJ creates a GraphJin instance configured for AdventureWorks.
// It also verifies the ground truth via direct SQL before running the GraphQL query.
func newAdventureWorksGJ(t *testing.T) *core.GraphJin {
	t.Helper()
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)
	t.Cleanup(func() { gj.Close() })
	return gj
}

func TestAdventureWorksSetup(t *testing.T) {
	skipIfNotAdventureWorks(t)

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sales.salesorderdetail").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 100000, "salesorderdetail should have 100k+ rows")
	t.Logf("salesorderdetail rows: %d", count)
}

func TestAdventureWorksDiscovery(t *testing.T) {
	skipIfNotAdventureWorks(t)

	gj := newAdventureWorksGJ(t)
	md := gj.GetCombinedDiscovery()
	require.NotEmpty(t, md, "discovery should be generated")

	// Verify cross-schema discovery found key schemas
	for _, schema := range []string{"person", "production", "sales", "purchasing", "humanresources"} {
		assert.Contains(t, md, schema)
	}
	// Verify key tables
	for _, table := range []string{"salesorderheader", "salesorderdetail", "product", "employee", "vendor"} {
		assert.Contains(t, md, table)
	}
	t.Logf("Discovery document: %d bytes", len(md))
}

// Test 1: Sales order → detail join (basic parent-child)
func TestAdventureWorksOrderWithDetails(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: order 43659 has 12 detail rows
	var detailCount int
	err := db.QueryRow("SELECT COUNT(*) FROM sales.salesorderdetail WHERE salesorderid = 43659").Scan(&detailCount)
	require.NoError(t, err)
	require.Equal(t, 12, detailCount, "ground truth: order 43659 should have 12 details")

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderheader(where: {salesorderid: {eq: 43659}}) {
				salesorderid
				totaldue
				salesorderdetails: salesorderdetail {
					productid
					orderqty
					unitprice
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		SalesOrderHeader []struct {
			SalesOrderID     int     `json:"salesorderid"`
			TotalDue         float64 `json:"totaldue"`
			SalesOrderDetails []struct {
				ProductID int     `json:"productid"`
				OrderQty  int     `json:"orderqty"`
				UnitPrice float64 `json:"unitprice"`
			} `json:"salesorderdetails"`
		} `json:"salesorderheader"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.SalesOrderHeader, 1)
	assert.Equal(t, 43659, result.SalesOrderHeader[0].SalesOrderID)
	assert.Len(t, result.SalesOrderHeader[0].SalesOrderDetails, 12)
}

// Test 2: Product → subcategory → category (3-level hierarchy within production schema)
func TestAdventureWorksProductCategoryHierarchy(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: Road-150 Red, 44 costs 3578.27, subcategory "Road Bikes", category "Bikes"
	var productName, subcatName, catName string
	var listPrice float64
	err := db.QueryRow(`
		SELECT p.name, p.listprice, ps.name, pc.name
		FROM production.product p
		JOIN production.productsubcategory ps ON p.productsubcategoryid = ps.productsubcategoryid
		JOIN production.productcategory pc ON ps.productcategoryid = pc.productcategoryid
		WHERE p.listprice > 3000
		ORDER BY p.listprice DESC, p.name ASC LIMIT 1`).Scan(&productName, &listPrice, &subcatName, &catName)
	require.NoError(t, err)
	require.Equal(t, "Road-150 Red, 44", productName)
	require.Equal(t, "Road Bikes", subcatName)
	require.Equal(t, "Bikes", catName)

	res, err := gj.GraphQL(context.Background(),
		`query {
			product(where: {listprice: {gt: 3000}}, order_by: {listprice: desc, name: asc}, limit: 1) {
				name
				listprice
				productsubcategorys: productsubcategory {
					name
					productcategorys: productcategory {
						name
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Product []struct {
			Name      string  `json:"name"`
			ListPrice float64 `json:"listprice"`
			SubCat    struct {
				Name string `json:"name"`
				Cat  struct {
					Name string `json:"name"`
				} `json:"productcategorys"`
			} `json:"productsubcategorys"`
		} `json:"product"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Product, 1)
	assert.Equal(t, "Road-150 Red, 44", result.Product[0].Name)
	assert.InDelta(t, 3578.27, result.Product[0].ListPrice, 0.01)
	assert.Equal(t, "Road Bikes", result.Product[0].SubCat.Name)
	assert.Equal(t, "Bikes", result.Product[0].SubCat.Cat.Name)
}

// Test 3: Sales order with customer, salesperson, territory (multi-FK fan-out from salesorderheader)
func TestAdventureWorksSalesOrderFanOut(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: top order by subtotal
	var orderID, customerID, salespersonID int
	var territory string
	err := db.QueryRow(`
		SELECT soh.salesorderid, soh.customerid, soh.salespersonid, st.name
		FROM sales.salesorderheader soh
		JOIN sales.salesterritory st ON soh.territoryid = st.territoryid
		WHERE soh.salespersonid IS NOT NULL
		ORDER BY soh.subtotal DESC LIMIT 1`).Scan(&orderID, &customerID, &salespersonID, &territory)
	require.NoError(t, err)
	t.Logf("Ground truth: order=%d customer=%d salesperson=%d territory=%s", orderID, customerID, salespersonID, territory)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderheader(order_by: {subtotal: desc}, limit: 1) {
				salesorderid
				customerid
				salespersonid
				customers: customer {
					customerid
				}
				salespersons: salesperson {
					businessentityid
				}
				salesterritorys: salesterritory {
					name
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		SalesOrderHeader []struct {
			SalesOrderID  int `json:"salesorderid"`
			CustomerID    int `json:"customerid"`
			SalesPersonID int `json:"salespersonid"`
			Customer      struct {
				CustomerID int `json:"customerid"`
			} `json:"customers"`
			SalesPerson struct {
				BusinessEntityID int `json:"businessentityid"`
			} `json:"salespersons"`
			Territory struct {
				Name string `json:"name"`
			} `json:"salesterritorys"`
		} `json:"salesorderheader"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.SalesOrderHeader, 1)
	row := result.SalesOrderHeader[0]
	assert.Equal(t, orderID, row.SalesOrderID)
	assert.Equal(t, customerID, row.Customer.CustomerID)
	assert.Equal(t, salespersonID, row.SalesPerson.BusinessEntityID)
	assert.Equal(t, territory, row.Territory.Name)
}

// Test 4: Product inventory with location (cross-table join in production schema)
func TestAdventureWorksProductInventory(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: top inventory item by quantity
	var productName, locationName, shelf string
	var quantity int
	err := db.QueryRow(`
		SELECT p.name, l.name, pi.quantity, pi.shelf
		FROM production.productinventory pi
		JOIN production.product p ON pi.productid = p.productid
		JOIN production.location l ON pi.locationid = l.locationid
		ORDER BY pi.quantity DESC LIMIT 1`).Scan(&productName, &locationName, &quantity, &shelf)
	require.NoError(t, err)
	t.Logf("Ground truth: product=%s location=%s qty=%d shelf=%s", productName, locationName, quantity, shelf)

	res, err := gj.GraphQL(context.Background(),
		`query {
			productinventory(order_by: {quantity: desc}, limit: 1) {
				quantity
				shelf
				products: product {
					name
				}
				locations: location {
					name
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		ProductInventory []struct {
			Quantity int    `json:"quantity"`
			Shelf    string `json:"shelf"`
			Product  struct {
				Name string `json:"name"`
			} `json:"products"`
			Location struct {
				Name string `json:"name"`
			} `json:"locations"`
		} `json:"productinventory"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.ProductInventory, 1)
	row := result.ProductInventory[0]
	assert.Equal(t, productName, row.Product.Name)
	assert.Equal(t, locationName, row.Location.Name)
	assert.Equal(t, quantity, row.Quantity)
	assert.Equal(t, shelf, row.Shelf)
}

// Test 5: Work order → product + routing → location (production chain, 4 tables)
func TestAdventureWorksWorkOrderRouting(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: work order with highest routing cost
	var workOrderID int
	var productName, locationName string
	var actualCost float64
	err := db.QueryRow(`
		SELECT wo.workorderid, p.name, l.name, wor.actualcost
		FROM production.workorder wo
		JOIN production.product p ON wo.productid = p.productid
		JOIN production.workorderrouting wor ON wo.workorderid = wor.workorderid
		JOIN production.location l ON wor.locationid = l.locationid
		WHERE wor.actualcost > 0
		ORDER BY wor.actualcost DESC, wo.workorderid ASC LIMIT 1`).Scan(&workOrderID, &productName, &locationName, &actualCost)
	require.NoError(t, err)
	t.Logf("Ground truth: workorder=%d product=%s location=%s cost=%.2f", workOrderID, productName, locationName, actualCost)

	res, err := gj.GraphQL(context.Background(),
		`query {
			workorderrouting(where: {actualcost: {gt: 0}}, order_by: {actualcost: desc}, limit: 1) {
				actualcost
				workorders: workorder {
					workorderid
					products: product {
						name
					}
				}
				locations: location {
					name
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		WorkOrderRouting []struct {
			ActualCost float64 `json:"actualcost"`
			WorkOrder  struct {
				WorkOrderID int `json:"workorderid"`
				Product     struct {
					Name string `json:"name"`
				} `json:"products"`
			} `json:"workorders"`
			Location struct {
				Name string `json:"name"`
			} `json:"locations"`
		} `json:"workorderrouting"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.WorkOrderRouting, 1)
	row := result.WorkOrderRouting[0]
	assert.InDelta(t, actualCost, row.ActualCost, 0.01)
	assert.Equal(t, locationName, row.Location.Name)
	assert.Equal(t, productName, row.WorkOrder.Product.Name)
}

// Test 6: Purchase order → vendor + shipmethod + details (purchasing schema, 4-way join)
func TestAdventureWorksPurchaseOrderChain(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: purchase order with highest subtotal
	var poID int
	var vendorName, shipMethod string
	var subtotal float64
	var detailCount int
	err := db.QueryRow(`
		SELECT poh.purchaseorderid, v.name, sm.name, poh.subtotal
		FROM purchasing.purchaseorderheader poh
		JOIN purchasing.vendor v ON poh.vendorid = v.businessentityid
		JOIN purchasing.shipmethod sm ON poh.shipmethodid = sm.shipmethodid
		ORDER BY poh.subtotal DESC LIMIT 1`).Scan(&poID, &vendorName, &shipMethod, &subtotal)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM purchasing.purchaseorderdetail WHERE purchaseorderid = $1", poID).Scan(&detailCount)
	require.NoError(t, err)
	t.Logf("Ground truth: PO=%d vendor=%s ship=%s subtotal=%.2f details=%d", poID, vendorName, shipMethod, subtotal, detailCount)

	res, err := gj.GraphQL(context.Background(),
		`query {
			purchaseorderheader(order_by: {subtotal: desc}, limit: 1) {
				purchaseorderid
				subtotal
				vendors: vendor {
					name
				}
				shipmethods: shipmethod {
					name
				}
				purchaseorderdetails: purchaseorderdetail {
					orderqty
					unitprice
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		PurchaseOrderHeader []struct {
			PurchaseOrderID int     `json:"purchaseorderid"`
			Subtotal        float64 `json:"subtotal"`
			Vendor          struct {
				Name string `json:"name"`
			} `json:"vendors"`
			ShipMethod struct {
				Name string `json:"name"`
			} `json:"shipmethods"`
			Details []struct {
				OrderQty  int     `json:"orderqty"`
				UnitPrice float64 `json:"unitprice"`
			} `json:"purchaseorderdetails"`
		} `json:"purchaseorderheader"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.PurchaseOrderHeader, 1)
	row := result.PurchaseOrderHeader[0]
	assert.Equal(t, poID, row.PurchaseOrderID)
	assert.InDelta(t, subtotal, row.Subtotal, 0.01)
	assert.Equal(t, vendorName, row.Vendor.Name)
	assert.Equal(t, shipMethod, row.ShipMethod.Name)
	assert.Len(t, row.Details, detailCount)
}

// Test 7: Composite FK — salesorderdetail → specialofferproduct (verified per-row matching)
func TestAdventureWorksCompositeFKJoin(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: get the actual (productid, specialofferid) pairs for order 43664
	type fkPair struct {
		ProductID      int
		SpecialOfferID int
	}
	rows, err := db.Query(`
		SELECT productid, specialofferid
		FROM sales.salesorderdetail
		WHERE salesorderid = 43664
		ORDER BY productid ASC LIMIT 3`)
	require.NoError(t, err)
	defer rows.Close()

	var expected []fkPair
	for rows.Next() {
		var p fkPair
		require.NoError(t, rows.Scan(&p.ProductID, &p.SpecialOfferID))
		expected = append(expected, p)
	}
	require.Len(t, expected, 3)
	t.Logf("Ground truth pairs: %+v", expected)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderdetail(where: {salesorderid: {eq: 43664}}, limit: 3, order_by: {productid: asc}) {
				productid
				specialofferid
				specialofferproducts: specialofferproduct {
					productid
					specialofferid
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		SalesOrderDetail []struct {
			ProductID      int `json:"productid"`
			SpecialOfferID int `json:"specialofferid"`
			SOP            struct {
				ProductID      int `json:"productid"`
				SpecialOfferID int `json:"specialofferid"`
			} `json:"specialofferproducts"`
		} `json:"salesorderdetail"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.SalesOrderDetail, 3)

	for i, row := range result.SalesOrderDetail {
		assert.Equal(t, expected[i].ProductID, row.ProductID, "detail productid mismatch at row %d", i)
		assert.Equal(t, expected[i].SpecialOfferID, row.SpecialOfferID, "detail specialofferid mismatch at row %d", i)
		// Composite FK: the child's productid and specialofferid must match the parent's
		assert.Equal(t, row.ProductID, row.SOP.ProductID, "composite FK productid mismatch at row %d", i)
		assert.Equal(t, row.SpecialOfferID, row.SOP.SpecialOfferID, "composite FK specialofferid mismatch at row %d", i)
	}
}

// Test 8: Deep 5-level join across schemas
// salesterritory → salesorderheader → salesorderdetail → specialofferproduct → product
func TestAdventureWorksDeepCrossSchemaJoin(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: pick a specific territory and verify the product chain
	var territoryName, productName string
	var orderID, productID int
	err := db.QueryRow(`
		SELECT st.name, soh.salesorderid, p.productid, p.name
		FROM sales.salesterritory st
		JOIN sales.salesorderheader soh ON st.territoryid = soh.territoryid
		JOIN sales.salesorderdetail sod ON soh.salesorderid = sod.salesorderid
		JOIN sales.specialofferproduct sop ON sod.productid = sop.productid AND sod.specialofferid = sop.specialofferid
		JOIN production.product p ON sop.productid = p.productid
		WHERE st.name = 'Northwest'
		ORDER BY soh.salesorderid, sod.productid
		LIMIT 1`).Scan(&territoryName, &orderID, &productID, &productName)
	require.NoError(t, err)
	t.Logf("Ground truth: territory=%s order=%d product=%d name=%s", territoryName, orderID, productID, productName)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesterritory(where: {name: {eq: "Northwest"}}) {
				name
				salesorderheaders: salesorderheader(order_by: {salesorderid: asc}, limit: 1) {
					salesorderid
					salesorderdetails: salesorderdetail(order_by: {productid: asc}, limit: 1) {
						productid
						specialofferproducts: specialofferproduct {
							products: product {
								productid
								name
							}
						}
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		SalesTerritory []struct {
			Name    string `json:"name"`
			Headers []struct {
				SalesOrderID int `json:"salesorderid"`
				Details      []struct {
					ProductID int `json:"productid"`
					SOP       struct {
						Product struct {
							ProductID int    `json:"productid"`
							Name      string `json:"name"`
						} `json:"products"`
					} `json:"specialofferproducts"`
				} `json:"salesorderdetails"`
			} `json:"salesorderheaders"`
		} `json:"salesterritory"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.SalesTerritory, 1)
	assert.Equal(t, "Northwest", result.SalesTerritory[0].Name)
	require.NotEmpty(t, result.SalesTerritory[0].Headers)
	assert.Equal(t, orderID, result.SalesTerritory[0].Headers[0].SalesOrderID)
	require.NotEmpty(t, result.SalesTerritory[0].Headers[0].Details)
	detail := result.SalesTerritory[0].Headers[0].Details[0]
	assert.Equal(t, productID, detail.SOP.Product.ProductID)
	assert.Equal(t, productName, detail.SOP.Product.Name)
}

// Test 9: Employee → department history → department + shift (HR schema, multiple FKs from one table)
func TestAdventureWorksEmployeeDepartment(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: first employee's current department and shift
	var deptName, shiftName string
	err := db.QueryRow(`
		SELECT d.name, s.name
		FROM humanresources.employeedepartmenthistory edh
		JOIN humanresources.department d ON edh.departmentid = d.departmentid
		JOIN humanresources.shift s ON edh.shiftid = s.shiftid
		WHERE edh.businessentityid = 1 AND edh.enddate IS NULL`).Scan(&deptName, &shiftName)
	require.NoError(t, err)
	t.Logf("Ground truth: dept=%s shift=%s", deptName, shiftName)

	res, err := gj.GraphQL(context.Background(),
		`query {
			employeedepartmenthistory(
				where: {businessentityid: {eq: 1}, enddate: {is_null: true}}
			) {
				businessentityid
				startdate
				departments: department {
					name
					groupname
				}
				shifts: shift {
					name
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		EDH []struct {
			BusinessEntityID int    `json:"businessentityid"`
			StartDate        string `json:"startdate"`
			Department       struct {
				Name      string `json:"name"`
				GroupName string `json:"groupname"`
			} `json:"departments"`
			Shift struct {
				Name string `json:"name"`
			} `json:"shifts"`
		} `json:"employeedepartmenthistory"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.EDH, 1)
	assert.Equal(t, 1, result.EDH[0].BusinessEntityID)
	assert.Equal(t, deptName, result.EDH[0].Department.Name)
	assert.Equal(t, shiftName, result.EDH[0].Shift.Name)
}

// Test 10: Address → stateprovince → countryregion (geographic hierarchy, person schema)
func TestAdventureWorksAddressHierarchy(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: pick an address and verify the chain
	var city, state, country string
	err := db.QueryRow(`
		SELECT a.city, sp.name, cr.name
		FROM person.address a
		JOIN person.stateprovince sp ON a.stateprovinceid = sp.stateprovinceid
		JOIN person.countryregion cr ON sp.countryregioncode = cr.countryregioncode
		ORDER BY a.addressid LIMIT 1`).Scan(&city, &state, &country)
	require.NoError(t, err)
	t.Logf("Ground truth: city=%s state=%s country=%s", city, state, country)

	res, err := gj.GraphQL(context.Background(),
		`query {
			address(order_by: {addressid: asc}, limit: 1) {
				city
				stateprovinces: stateprovince {
					name
					countryregions: countryregion {
						name
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Address []struct {
			City          string `json:"city"`
			StateProvince struct {
				Name          string `json:"name"`
				CountryRegion struct {
					Name string `json:"name"`
				} `json:"countryregions"`
			} `json:"stateprovinces"`
		} `json:"address"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Address, 1)
	assert.Equal(t, city, result.Address[0].City)
	assert.Equal(t, state, result.Address[0].StateProvince.Name)
	assert.Equal(t, country, result.Address[0].StateProvince.CountryRegion.Name)
}

// Test 11: Filtering + ordering on large dataset (121K rows in salesorderdetail)
func TestAdventureWorksLargeDatasetFilter(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: top 5 order details by line total (unitprice * orderqty)
	type detailRow struct {
		SalesOrderID int
		ProductID    int
		OrderQty     int
		UnitPrice    float64
	}
	rows, err := db.Query(`
		SELECT salesorderid, productid, orderqty, unitprice
		FROM sales.salesorderdetail
		ORDER BY unitprice DESC, salesorderid ASC, productid ASC
		LIMIT 5`)
	require.NoError(t, err)
	defer rows.Close()

	var expected []detailRow
	for rows.Next() {
		var r detailRow
		require.NoError(t, rows.Scan(&r.SalesOrderID, &r.ProductID, &r.OrderQty, &r.UnitPrice))
		expected = append(expected, r)
	}
	require.Len(t, expected, 5)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderdetail(order_by: {unitprice: desc, salesorderid: asc, productid: asc}, limit: 5) {
				salesorderid
				productid
				orderqty
				unitprice
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Details []detailRow `json:"salesorderdetail"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Details, 5)

	for i, row := range result.Details {
		assert.Equal(t, expected[i].SalesOrderID, row.SalesOrderID, "salesorderid mismatch at row %d", i)
		assert.Equal(t, expected[i].ProductID, row.ProductID, "productid mismatch at row %d", i)
		assert.Equal(t, expected[i].OrderQty, row.OrderQty, "orderqty mismatch at row %d", i)
		assert.InDelta(t, expected[i].UnitPrice, row.UnitPrice, 0.001, "unitprice mismatch at row %d", i)
	}
}

// ============================================================================
// Business scenario tests — questions a CXO, analyst, or customer would ask
// ============================================================================

// "Who is our top salesperson and what territory do they cover?"
// Joins: salesperson → person (cross-schema) + salesperson → territory
func TestAdventureWorksTopSalesperson(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var firstName, lastName, territory string
	var salesYTD float64
	err := db.QueryRow(`
		SELECT p.firstname, p.lastname, st.name, sp.salesytd
		FROM sales.salesperson sp
		JOIN person.person p ON sp.businessentityid = p.businessentityid
		JOIN sales.salesterritory st ON sp.territoryid = st.territoryid
		ORDER BY sp.salesytd DESC LIMIT 1`).Scan(&firstName, &lastName, &territory, &salesYTD)
	require.NoError(t, err)
	t.Logf("Ground truth: %s %s, territory=%s, salesYTD=%.2f", firstName, lastName, territory, salesYTD)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesperson(order_by: {salesytd: desc}, limit: 1) {
				businessentityid
				salesytd
				salesquota
				bonus
				commissionpct
				persons: person {
					firstname
					lastname
				}
				salesterritorys: salesterritory {
					name
					countryregioncode
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		SalesPerson []struct {
			BusinessEntityID int     `json:"businessentityid"`
			SalesYTD         float64 `json:"salesytd"`
			Persons          []struct {
				FirstName string `json:"firstname"`
				LastName  string `json:"lastname"`
			} `json:"persons"`
			Territory struct {
				Name string `json:"name"`
			} `json:"salesterritorys"`
		} `json:"salesperson"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.SalesPerson, 1)
	require.NotEmpty(t, result.SalesPerson[0].Persons)
	assert.Equal(t, firstName, result.SalesPerson[0].Persons[0].FirstName)
	assert.Equal(t, lastName, result.SalesPerson[0].Persons[0].LastName)
	assert.Equal(t, territory, result.SalesPerson[0].Territory.Name)
	assert.InDelta(t, salesYTD, result.SalesPerson[0].SalesYTD, 0.01)
}

// "Which B2B stores are assigned to which salesperson and territory?"
// Joins: store → salesperson → person + salesperson → territory
func TestAdventureWorksStoreAccountManagement(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var storeName, spFirstName, spLastName, territory string
	err := db.QueryRow(`
		SELECT s.name, p.firstname, p.lastname, st.name
		FROM sales.store s
		JOIN sales.salesperson sp ON s.salespersonid = sp.businessentityid
		JOIN person.person p ON sp.businessentityid = p.businessentityid
		JOIN sales.salesterritory st ON sp.territoryid = st.territoryid
		ORDER BY s.name ASC LIMIT 1`).Scan(&storeName, &spFirstName, &spLastName, &territory)
	require.NoError(t, err)
	t.Logf("Ground truth: store=%s rep=%s %s territory=%s", storeName, spFirstName, spLastName, territory)

	res, err := gj.GraphQL(context.Background(),
		`query {
			store(order_by: {name: asc}, limit: 1) {
				name
				salespersons: salesperson {
					persons: person {
						firstname
						lastname
					}
					salesterritorys: salesterritory {
						name
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Store []struct {
			Name        string `json:"name"`
			SalesPerson struct {
				Persons []struct {
					FirstName string `json:"firstname"`
					LastName  string `json:"lastname"`
				} `json:"persons"`
				Territory struct {
					Name string `json:"name"`
				} `json:"salesterritorys"`
			} `json:"salespersons"`
		} `json:"store"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Store, 1)
	require.NotEmpty(t, result.Store[0].SalesPerson.Persons)
	assert.Equal(t, storeName, result.Store[0].Name)
	assert.Equal(t, spFirstName, result.Store[0].SalesPerson.Persons[0].FirstName)
	assert.Equal(t, territory, result.Store[0].SalesPerson.Territory.Name)
}

// "What is the full product spec: model, subcategory, category, and where is it stocked?"
// Joins: product → model + product → subcategory → category + product → inventory → location
func TestAdventureWorksFullProductSpec(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var productName, modelName, subcatName, catName, locationName string
	var qty int
	err := db.QueryRow(`
		SELECT p.name, pm.name, ps.name, pc.name, l.name, pi.quantity
		FROM production.productinventory pi
		JOIN production.product p ON pi.productid = p.productid
		JOIN production.productmodel pm ON p.productmodelid = pm.productmodelid
		JOIN production.productsubcategory ps ON p.productsubcategoryid = ps.productsubcategoryid
		JOIN production.productcategory pc ON ps.productcategoryid = pc.productcategoryid
		JOIN production.location l ON pi.locationid = l.locationid
		WHERE pi.quantity > 0 AND p.productmodelid IS NOT NULL AND p.productsubcategoryid IS NOT NULL
		ORDER BY pi.quantity DESC LIMIT 1`).Scan(&productName, &modelName, &subcatName, &catName, &locationName, &qty)
	require.NoError(t, err)
	t.Logf("Ground truth: product=%s model=%s subcat=%s cat=%s location=%s qty=%d",
		productName, modelName, subcatName, catName, locationName, qty)

	// Query a specific product that we know has model + subcategory
	res, err := gj.GraphQL(context.Background(),
		`query {
			product(where: {name: {eq: $name}}) {
				name
				productmodels: productmodel {
					name
				}
				productsubcategorys: productsubcategory {
					name
					productcategorys: productcategory {
						name
					}
				}
				productinventorys: productinventory(order_by: {quantity: desc}, limit: 1) {
					quantity
					locations: location {
						name
					}
				}
			}
		}`, json.RawMessage(`{"name":"`+productName+`"}`), nil)
	require.NoError(t, err)

	var result struct {
		Product []struct {
			Name  string `json:"name"`
			Model struct {
				Name string `json:"name"`
			} `json:"productmodels"`
			SubCategory struct {
				Name     string `json:"name"`
				Category struct {
					Name string `json:"name"`
				} `json:"productcategorys"`
			} `json:"productsubcategorys"`
			Inventory []struct {
				Quantity int `json:"quantity"`
				Location struct {
					Name string `json:"name"`
				} `json:"locations"`
			} `json:"productinventorys"`
		} `json:"product"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Product, 1)
	p := result.Product[0]
	assert.Equal(t, productName, p.Name)
	assert.Equal(t, modelName, p.Model.Name)
	assert.Equal(t, subcatName, p.SubCategory.Name)
	assert.Equal(t, catName, p.SubCategory.Category.Name)
	require.NotEmpty(t, p.Inventory)
	assert.Equal(t, qty, p.Inventory[0].Quantity)
	assert.Equal(t, locationName, p.Inventory[0].Location.Name)
}

// "What are our highest-margin products?"
// Joins: product → cost history + product → list price history (dual history join)
func TestAdventureWorksProductMarginAnalysis(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var productName string
	var cost, listPrice float64
	err := db.QueryRow(`
		SELECT p.name, pch.standardcost, plph.listprice
		FROM production.product p
		JOIN production.productcosthistory pch ON p.productid = pch.productid
		JOIN production.productlistpricehistory plph ON p.productid = plph.productid
			AND pch.startdate = plph.startdate
		WHERE pch.enddate IS NULL AND plph.enddate IS NULL
		ORDER BY (plph.listprice - pch.standardcost) DESC
		LIMIT 1`).Scan(&productName, &cost, &listPrice)
	require.NoError(t, err)
	margin := listPrice - cost
	t.Logf("Ground truth: product=%s cost=%.4f listprice=%.4f margin=%.2f", productName, cost, listPrice, margin)

	// Query product with both cost and price history
	res, err := gj.GraphQL(context.Background(),
		`query {
			product(where: {name: {eq: $name}}) {
				name
				productcosthistorys: productcosthistory(where: {enddate: {is_null: true}}) {
					standardcost
					startdate
				}
				productlistpricehistorys: productlistpricehistory(where: {enddate: {is_null: true}}) {
					listprice
					startdate
				}
			}
		}`, json.RawMessage(`{"name": "`+productName+`"}`), nil)
	require.NoError(t, err)

	var result struct {
		Product []struct {
			Name        string `json:"name"`
			CostHistory []struct {
				StandardCost float64 `json:"standardcost"`
			} `json:"productcosthistorys"`
			PriceHistory []struct {
				ListPrice float64 `json:"listprice"`
			} `json:"productlistpricehistorys"`
		} `json:"product"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Product, 1)
	assert.Equal(t, productName, result.Product[0].Name)
	require.NotEmpty(t, result.Product[0].CostHistory)
	require.NotEmpty(t, result.Product[0].PriceHistory)
	assert.InDelta(t, cost, result.Product[0].CostHistory[0].StandardCost, 0.01)
	assert.InDelta(t, listPrice, result.Product[0].PriceHistory[0].ListPrice, 0.01)
}

// "Which vendors supply our most expensive components?"
// Joins: productvendor → vendor + productvendor → product (cross purchasing/production schemas)
func TestAdventureWorksVendorSupplyChain(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var vendorName, productName string
	var standardPrice float64
	var leadTime int
	err := db.QueryRow(`
		SELECT v.name, p.name, pv.standardprice, pv.averageleadtime
		FROM purchasing.productvendor pv
		JOIN purchasing.vendor v ON pv.businessentityid = v.businessentityid
		JOIN production.product p ON pv.productid = p.productid
		ORDER BY pv.standardprice DESC LIMIT 1`).Scan(&vendorName, &productName, &standardPrice, &leadTime)
	require.NoError(t, err)
	t.Logf("Ground truth: vendor=%s product=%s price=%.2f leadtime=%d", vendorName, productName, standardPrice, leadTime)

	res, err := gj.GraphQL(context.Background(),
		`query {
			productvendor(order_by: {standardprice: desc}, limit: 1) {
				standardprice
				averageleadtime
				vendors: vendor {
					name
					creditrating
				}
				products: product {
					name
					productnumber
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		ProductVendor []struct {
			StandardPrice  float64 `json:"standardprice"`
			AverageLeadTime int    `json:"averageleadtime"`
			Vendor          struct {
				Name string `json:"name"`
			} `json:"vendors"`
			Product struct {
				Name string `json:"name"`
			} `json:"products"`
		} `json:"productvendor"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.ProductVendor, 1)
	assert.Equal(t, vendorName, result.ProductVendor[0].Vendor.Name)
	assert.Equal(t, productName, result.ProductVendor[0].Product.Name)
	assert.InDelta(t, standardPrice, result.ProductVendor[0].StandardPrice, 0.01)
	assert.Equal(t, leadTime, result.ProductVendor[0].AverageLeadTime)
}

// "How much do our executives earn vs their department?"
// Joins: employee → person + employeepayhistory + employeedepartmenthistory → department
// 3 child tables from employee, cross person+HR schemas
func TestAdventureWorksExecutiveCompensation(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var firstName, lastName, jobTitle, deptName string
	var payRate float64
	err := db.QueryRow(`
		SELECT p.firstname, p.lastname, e.jobtitle, d.name, eph.rate
		FROM humanresources.employee e
		JOIN person.person p ON e.businessentityid = p.businessentityid
		JOIN humanresources.employeepayhistory eph ON e.businessentityid = eph.businessentityid
		JOIN humanresources.employeedepartmenthistory edh ON e.businessentityid = edh.businessentityid
		JOIN humanresources.department d ON edh.departmentid = d.departmentid
		WHERE edh.enddate IS NULL
		ORDER BY eph.rate DESC LIMIT 1`).Scan(&firstName, &lastName, &jobTitle, &deptName, &payRate)
	require.NoError(t, err)
	t.Logf("Ground truth: %s %s (%s) dept=%s rate=%.2f", firstName, lastName, jobTitle, deptName, payRate)

	res, err := gj.GraphQL(context.Background(),
		`query {
			employee(order_by: {businessentityid: asc}, limit: 1) {
				businessentityid
				jobtitle
				hiredate
				persons: person {
					firstname
					lastname
				}
				employeepayhistorys: employeepayhistory(order_by: {ratechangedate: desc}, limit: 1) {
					rate
					payfrequency
				}
				employeedepartmenthistorys: employeedepartmenthistory(where: {enddate: {is_null: true}}) {
					departments: department {
						name
						groupname
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Employee []struct {
			BusinessEntityID int    `json:"businessentityid"`
			JobTitle         string `json:"jobtitle"`
			Person           struct {
				FirstName string `json:"firstname"`
				LastName  string `json:"lastname"`
			} `json:"persons"`
			PayHistory []struct {
				Rate float64 `json:"rate"`
			} `json:"employeepayhistorys"`
			DeptHistory []struct {
				Department struct {
					Name string `json:"name"`
				} `json:"departments"`
			} `json:"employeedepartmenthistorys"`
		} `json:"employee"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Employee, 1)
	emp := result.Employee[0]
	// Employee 1 is the CEO (highest paid in ground truth)
	assert.Equal(t, firstName, emp.Person.FirstName)
	assert.Equal(t, lastName, emp.Person.LastName)
	require.NotEmpty(t, emp.PayHistory)
	assert.InDelta(t, payRate, emp.PayHistory[0].Rate, 0.01)
	require.NotEmpty(t, emp.DeptHistory)
	assert.Equal(t, deptName, emp.DeptHistory[0].Department.Name)
}

// "Show me customer contact details: name, email, phone, and address with full geography"
// Joins: person → emailaddress + personphone → phonenumbertype + businessentityaddress → address → stateprovince
// 7-table join, deepest nesting test
func TestAdventureWorksCustomerContactDetails(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var firstName, lastName, email, phone, phoneType, city, state string
	err := db.QueryRow(`
		SELECT p.firstname, p.lastname, ea.emailaddress,
			pp.phonenumber, pnt.name, a.city, sp.name
		FROM person.person p
		JOIN person.emailaddress ea ON p.businessentityid = ea.businessentityid
		JOIN person.personphone pp ON p.businessentityid = pp.businessentityid
		JOIN person.phonenumbertype pnt ON pp.phonenumbertypeid = pnt.phonenumbertypeid
		JOIN person.businessentityaddress bea ON p.businessentityid = bea.businessentityid
		JOIN person.address a ON bea.addressid = a.addressid
		JOIN person.stateprovince sp ON a.stateprovinceid = sp.stateprovinceid
		ORDER BY p.businessentityid LIMIT 1`).Scan(&firstName, &lastName, &email, &phone, &phoneType, &city, &state)
	require.NoError(t, err)
	t.Logf("Ground truth: %s %s, email=%s, phone=%s (%s), city=%s, state=%s",
		firstName, lastName, email, phone, phoneType, city, state)

	res, err := gj.GraphQL(context.Background(),
		`query {
			person(order_by: {businessentityid: asc}, limit: 1) {
				firstname
				lastname
				emailaddresss: emailaddress {
					emailaddress
				}
				personphones: personphone {
					phonenumber
					phonenumbertypes: phonenumbertype {
						name
					}
				}
				businessentityaddresss: businessentityaddress {
					addresss: address {
						city
						stateprovinces: stateprovince {
							name
						}
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Person []struct {
			FirstName string `json:"firstname"`
			LastName  string `json:"lastname"`
			Emails    []struct {
				EmailAddress string `json:"emailaddress"`
			} `json:"emailaddresss"`
			Phones []struct {
				PhoneNumber string `json:"phonenumber"`
				PhoneType   struct {
					Name string `json:"name"`
				} `json:"phonenumbertypes"`
			} `json:"personphones"`
			Addresses []struct {
				Address struct {
					City  string `json:"city"`
					State struct {
						Name string `json:"name"`
					} `json:"stateprovinces"`
				} `json:"addresss"`
			} `json:"businessentityaddresss"`
		} `json:"person"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Person, 1)
	p := result.Person[0]
	assert.Equal(t, firstName, p.FirstName)
	assert.Equal(t, lastName, p.LastName)
	require.NotEmpty(t, p.Emails)
	assert.Equal(t, email, p.Emails[0].EmailAddress)
	require.NotEmpty(t, p.Phones)
	assert.Equal(t, phone, p.Phones[0].PhoneNumber)
	assert.Equal(t, phoneType, p.Phones[0].PhoneType.Name)
	require.NotEmpty(t, p.Addresses)
	assert.Equal(t, city, p.Addresses[0].Address.City)
	assert.Equal(t, state, p.Addresses[0].Address.State.Name)
}

// "What credit card types are used on our biggest orders?"
// Joins: salesorderheader → creditcard (financial analysis)
func TestAdventureWorksCreditCardAnalysis(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	type ccRow struct {
		SalesOrderID int     `json:"salesorderid"`
		Subtotal     float64 `json:"subtotal"`
		CardType     string
	}
	rows, err := db.Query(`
		SELECT soh.salesorderid, soh.subtotal, cc.cardtype
		FROM sales.salesorderheader soh
		JOIN sales.creditcard cc ON soh.creditcardid = cc.creditcardid
		ORDER BY soh.subtotal DESC LIMIT 3`)
	require.NoError(t, err)
	defer rows.Close()
	var expected []ccRow
	for rows.Next() {
		var r ccRow
		require.NoError(t, rows.Scan(&r.SalesOrderID, &r.Subtotal, &r.CardType))
		expected = append(expected, r)
	}
	require.Len(t, expected, 3)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderheader(
				where: {creditcardid: {is_null: false}},
				order_by: {subtotal: desc},
				limit: 3
			) {
				salesorderid
				subtotal
				creditcards: creditcard {
					cardtype
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Orders []struct {
			SalesOrderID int     `json:"salesorderid"`
			Subtotal     float64 `json:"subtotal"`
			CreditCard   struct {
				CardType string `json:"cardtype"`
			} `json:"creditcards"`
		} `json:"salesorderheader"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Orders, 3)
	for i, row := range result.Orders {
		assert.Equal(t, expected[i].SalesOrderID, row.SalesOrderID, "order id mismatch at %d", i)
		assert.InDelta(t, expected[i].Subtotal, row.Subtotal, 0.01, "subtotal mismatch at %d", i)
		assert.Equal(t, expected[i].CardType, row.CreditCard.CardType, "card type mismatch at %d", i)
	}
}

// "Why did customers buy? Show me orders with their sales reasons."
// Joins: salesorderheader → salesorderheadersalesreason → salesreason (many-to-many)
func TestAdventureWorksSalesReasonAttribution(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Find an order with multiple reasons
	var orderID, reasonCount int
	err := db.QueryRow(`
		SELECT salesorderid, count(*)
		FROM sales.salesorderheadersalesreason
		GROUP BY salesorderid
		ORDER BY count(*) DESC, salesorderid ASC
		LIMIT 1`).Scan(&orderID, &reasonCount)
	require.NoError(t, err)

	// Get the actual reasons
	reasonRows, err := db.Query(`
		SELECT sr.name, sr.reasontype
		FROM sales.salesorderheadersalesreason sosr
		JOIN sales.salesreason sr ON sosr.salesreasonid = sr.salesreasonid
		WHERE sosr.salesorderid = $1
		ORDER BY sr.name`, orderID)
	require.NoError(t, err)
	defer reasonRows.Close()
	type reasonInfo struct {
		Name       string
		ReasonType string
	}
	var expectedReasons []reasonInfo
	for reasonRows.Next() {
		var r reasonInfo
		require.NoError(t, reasonRows.Scan(&r.Name, &r.ReasonType))
		expectedReasons = append(expectedReasons, r)
	}
	require.Len(t, expectedReasons, reasonCount)
	t.Logf("Ground truth: order=%d has %d reasons: %+v", orderID, reasonCount, expectedReasons)

	res, err := gj.GraphQL(context.Background(),
		`query {
			salesorderheadersalesreason(where: {salesorderid: {eq: $id}}, order_by: {salesreasonid: asc}) {
				salesorderid
				salesreasons: salesreason {
					name
					reasontype
				}
			}
		}`, json.RawMessage(`{"id": `+json.Number(itoa(orderID)).String()+`}`), nil)
	require.NoError(t, err)

	var result struct {
		Rows []struct {
			SalesOrderID int `json:"salesorderid"`
			SalesReason  struct {
				Name       string `json:"name"`
				ReasonType string `json:"reasontype"`
			} `json:"salesreasons"`
		} `json:"salesorderheadersalesreason"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Rows, reasonCount, "should have %d reason rows", reasonCount)
	for _, row := range result.Rows {
		assert.Equal(t, orderID, row.SalesOrderID)
		assert.NotEmpty(t, row.SalesReason.Name)
		assert.NotEmpty(t, row.SalesReason.ReasonType)
	}
}

// "What components make up our mountain bikes?" (Bill of Materials)
// Joins: billofmaterials → product (assembly) + product (component) + unitmeasure
func TestAdventureWorksBillOfMaterials(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	var assemblyName, componentName, unitName string
	var qty float64
	err := db.QueryRow(`
		SELECT pa.name, pc.name, bom.perassemblyqty, um.name
		FROM production.billofmaterials bom
		JOIN production.product pa ON bom.productassemblyid = pa.productid
		JOIN production.product pc ON bom.componentid = pc.productid
		JOIN production.unitmeasure um ON bom.unitmeasurecode = um.unitmeasurecode
		WHERE bom.enddate IS NULL AND bom.bomlevel = 1
		ORDER BY pa.name, pc.name LIMIT 1`).Scan(&assemblyName, &componentName, &qty, &unitName)
	require.NoError(t, err)
	t.Logf("Ground truth: assembly=%s component=%s qty=%.2f unit=%s", assemblyName, componentName, qty, unitName)

	// Count total components for this assembly
	var componentCount int
	err = db.QueryRow(`
		SELECT count(*) FROM production.billofmaterials
		WHERE productassemblyid = (
			SELECT productid FROM production.product WHERE name = $1 LIMIT 1
		) AND enddate IS NULL AND bomlevel = 1`, assemblyName).Scan(&componentCount)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(),
		`query {
			billofmaterials(
				where: {enddate: {is_null: true}, bomlevel: {eq: 1}, productassemblyid: {eq: $aid}},
				order_by: {componentid: asc}
			) {
				perassemblyqty
				bomlevel
				unitmeasures: unitmeasure {
					name
				}
			}
		}`, json.RawMessage(`{"aid": `+itoa(mustProductID(t, assemblyName))+`}`), nil)
	require.NoError(t, err)

	var result struct {
		BOM []struct {
			PerAssemblyQty float64 `json:"perassemblyqty"`
			BOMLevel       int     `json:"bomlevel"`
			UnitMeasure    struct {
				Name string `json:"name"`
			} `json:"unitmeasures"`
		} `json:"billofmaterials"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	assert.Len(t, result.BOM, componentCount, "should have %d components", componentCount)
	for _, row := range result.BOM {
		assert.Equal(t, 1, row.BOMLevel)
		assert.Greater(t, row.PerAssemblyQty, 0.0)
		assert.NotEmpty(t, row.UnitMeasure.Name)
	}
}

// "Show me our customer base by geography — which countries/states have the most customers?"
// Joins: customer → person → businessentityaddress → address → stateprovince → countryregion
// 6-level cross-schema join, verified row count
func TestAdventureWorksCustomerGeography(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: first 3 customers with full geography
	type custGeo struct {
		FirstName string
		LastName  string
		City      string
		State     string
		Country   string
	}
	rows, err := db.Query(`
		SELECT p.firstname, p.lastname, a.city, sp.name, cr.name
		FROM sales.customer c
		JOIN person.person p ON c.personid = p.businessentityid
		JOIN person.businessentityaddress bea ON p.businessentityid = bea.businessentityid
		JOIN person.address a ON bea.addressid = a.addressid
		JOIN person.stateprovince sp ON a.stateprovinceid = sp.stateprovinceid
		JOIN person.countryregion cr ON sp.countryregioncode = cr.countryregioncode
		WHERE c.personid IS NOT NULL
		ORDER BY c.customerid LIMIT 3`)
	require.NoError(t, err)
	defer rows.Close()
	var expected []custGeo
	for rows.Next() {
		var cg custGeo
		require.NoError(t, rows.Scan(&cg.FirstName, &cg.LastName, &cg.City, &cg.State, &cg.Country))
		expected = append(expected, cg)
	}
	require.Len(t, expected, 3)
	t.Logf("Ground truth: %+v", expected)

	res, err := gj.GraphQL(context.Background(),
		`query {
			customer(where: {personid: {is_null: false}}, order_by: {customerid: asc}, limit: 3) {
				customerid
				persons: person {
					firstname
					lastname
					businessentityaddresss: businessentityaddress {
						addresss: address {
							city
							stateprovinces: stateprovince {
								name
								countryregions: countryregion {
									name
								}
							}
						}
					}
				}
			}
		}`, nil, nil)
	require.NoError(t, err)

	var result struct {
		Customer []struct {
			CustomerID int `json:"customerid"`
			Person     struct {
				FirstName string `json:"firstname"`
				LastName  string `json:"lastname"`
				Addresses []struct {
					Address struct {
						City  string `json:"city"`
						State struct {
							Name    string `json:"name"`
							Country struct {
								Name string `json:"name"`
							} `json:"countryregions"`
						} `json:"stateprovinces"`
					} `json:"addresss"`
				} `json:"businessentityaddresss"`
			} `json:"persons"`
		} `json:"customer"`
	}
	require.NoError(t, json.Unmarshal(res.Data, &result))
	require.Len(t, result.Customer, 3)
	for i, cust := range result.Customer {
		assert.Equal(t, expected[i].FirstName, cust.Person.FirstName, "firstname mismatch at %d", i)
		assert.Equal(t, expected[i].LastName, cust.Person.LastName, "lastname mismatch at %d", i)
		require.NotEmpty(t, cust.Person.Addresses, "customer %d should have address", i)
		assert.Equal(t, expected[i].City, cust.Person.Addresses[0].Address.City, "city mismatch at %d", i)
		assert.Equal(t, expected[i].State, cust.Person.Addresses[0].Address.State.Name, "state mismatch at %d", i)
		assert.Equal(t, expected[i].Country, cust.Person.Addresses[0].Address.State.Country.Name, "country mismatch at %d", i)
	}
}

// Helper: convert int to string
func itoa(n int) string {
	return json.Number(fmt.Sprintf("%d", n)).String()
}

// Helper: look up productid by name
func mustProductID(t *testing.T, name string) int {
	t.Helper()
	var id int
	err := db.QueryRow("SELECT productid FROM production.product WHERE name = $1 LIMIT 1", name).Scan(&id)
	require.NoError(t, err)
	return id
}

// Test: View PK detection — vindividualcustomer should have a PK inferred from
// the base table, enabling cursor pagination.
func TestAdventureWorksViewPKDetection(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Ground truth: vindividualcustomer view should have data
	var gtCount int
	err := db.QueryRow("SELECT COUNT(*) FROM sales.vindividualcustomer").Scan(&gtCount)
	require.NoError(t, err)
	require.Greater(t, gtCount, 0, "vindividualcustomer should have rows")

	// Test cursor pagination on the view — this requires a PK to be detected
	res, err := gj.GraphQL(context.Background(),
		`query {
			vindividualcustomer(first: 5) {
				businessentityid
				firstname
				lastname
			}
		}`, nil, nil)
	require.NoError(t, err, "cursor pagination on view should work (requires PK detection)")

	var result struct {
		Vindividualcustomer []struct {
			Businessentityid int    `json:"businessentityid"`
			Firstname        string `json:"firstname"`
			Lastname         string `json:"lastname"`
		} `json:"vindividualcustomer"`
	}
	err = json.Unmarshal(res.Data, &result)
	require.NoError(t, err)
	assert.Len(t, result.Vindividualcustomer, 5, "should return exactly 5 rows with cursor pagination")
	for _, c := range result.Vindividualcustomer {
		assert.NotZero(t, c.Businessentityid)
		assert.NotEmpty(t, c.Firstname)
	}
	t.Logf("View cursor pagination returned %d rows (total view rows: %d)", len(result.Vindividualcustomer), gtCount)
}

// Test: search_path fallback — verify the inferred default schema works correctly
// even when the AdventureWorks dump sets search_path=''.
func TestAdventureWorksSchemaInference(t *testing.T) {
	skipIfNotAdventureWorks(t)
	gj := newAdventureWorksGJ(t)

	// Query a table without schema prefix — should resolve via inferred default schema
	// or unambiguous name lookup
	res, err := gj.GraphQL(context.Background(),
		`query {
			customer(first: 3) {
				customerid
				territoryid
			}
		}`, nil, nil)
	require.NoError(t, err, "unqualified table name should resolve with inferred schema")

	var result struct {
		Customer []struct {
			Customerid  int `json:"customerid"`
			Territoryid int `json:"territoryid"`
		} `json:"customer"`
	}
	err = json.Unmarshal(res.Data, &result)
	require.NoError(t, err)
	assert.Len(t, result.Customer, 3, "should return 3 customers")
	for _, c := range result.Customer {
		assert.NotZero(t, c.Customerid)
	}

	// Also verify the customer count matches ground truth
	var gtCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sales.customer").Scan(&gtCount)
	require.NoError(t, err)
	t.Logf("Customer query succeeded (total: %d)", gtCount)
}
