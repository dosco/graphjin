package sdata

import "testing"

func TestDiscoveryHashTracksColumns(t *testing.T) {
	base := []DBColumn{{ID: 1, Schema: "public", Table: "da", Name: "id", OrigName: "Id", Type: "int"}, {ID: 2, Schema: "public", Table: "da", Name: "name", OrigName: "Name", Type: "text"}}
	hash := func(cols []DBColumn) int {
		return NewDBInfo("snowflake", 1, "public", "analytics", cols, nil, nil).Hash()
	}
	original := hash(base)
	for _, change := range []string{"add", "drop", "rename", "case", "type", "key"} {
		t.Run(change, func(t *testing.T) {
			cols := append([]DBColumn(nil), base...)
			switch change {
			case "add":
				cols = append(cols, DBColumn{Schema: "public", Table: "da", Name: "email", Type: "text"})
			case "drop":
				cols = cols[:1]
			case "rename":
				cols[0].Name = "other_id"
			case "case":
				cols[0].OrigName = "id"
			case "type":
				cols[0].Type = "text"
			case "key":
				cols[0].PrimaryKey = true
			}
			if hash(cols) == original {
				t.Fatal("schema change did not change discovery hash")
			}
		})
	}
	reordered := []DBColumn{base[1], base[0]}
	reordered[0].ID = 90
	reordered[1].ID = 91
	if hash(reordered) != original {
		t.Fatal("discovery order changed hash")
	}
}
