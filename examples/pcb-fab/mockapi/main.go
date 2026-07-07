package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type part struct {
	SKU               string `json:"sku"`
	Name              string `json:"name"`
	Lifecycle         string `json:"lifecycle"`
	InventoryQty      int    `json:"inventory_qty"`
	LeadTimeDays      int    `json:"lead_time_days"`
	PreferredSupplier string `json:"preferred_supplier"`
}

var parts = map[string]part{
	"CAP-0402-X7R-100N": {SKU: "CAP-0402-X7R-100N", Name: "100 nF X7R capacitor", Lifecycle: "active", InventoryQty: 38000, LeadTimeDays: 5, PreferredSupplier: "Northwest Components"},
	"IC-AFE-8CH-MED":    {SKU: "IC-AFE-8CH-MED", Name: "8-channel medical analog front end", Lifecycle: "allocation", InventoryQty: 120, LeadTimeDays: 21, PreferredSupplier: "Apex Silicon"},
	"CONN-MICRO-40P":    {SKU: "CONN-MICRO-40P", Name: "40-pin micro connector", Lifecycle: "active", InventoryQty: 980, LeadTimeDays: 12, PreferredSupplier: "Cascade Interconnect"},
	"MOSFET-60V-30A":    {SKU: "MOSFET-60V-30A", Name: "60V 30A motor MOSFET", Lifecycle: "active", InventoryQty: 4200, LeadTimeDays: 8, PreferredSupplier: "Apex Silicon"},
	"RF-MOD-2G4":        {SKU: "RF-MOD-2G4", Name: "2.4 GHz radio module", Lifecycle: "nrnd", InventoryQty: 240, LeadTimeDays: 28, PreferredSupplier: "Harbor RF"},
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/parts/", func(w http.ResponseWriter, r *http.Request) {
		sku := strings.TrimPrefix(r.URL.Path, "/parts/")
		p, ok := parts[sku]
		if !ok {
			http.Error(w, `{"error":"part not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]part{"data": p})
	})

	log.Println("supplier mock API listening on :8093")
	log.Fatal(http.ListenAndServe(":8093", mux))
}
