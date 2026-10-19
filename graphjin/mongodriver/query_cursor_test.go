package mongodriver

import "testing"

func TestNormalizeCursorForSeek(t *testing.T) {
	tests := []struct {
		name string
		info *CursorInfo
		in   string
		want string
	}{
		{
			name: "already normalized",
			info: &CursorInfo{Prefix: "gj-123:"},
			in:   "12:100:99",
			want: "12:100:99",
		},
		{
			name: "strip exact prefix",
			info: &CursorInfo{Prefix: "gj-123:"},
			in:   "gj-123:12:100:99",
			want: "12:100:99",
		},
		{
			name: "fallback strip gj prefix",
			info: &CursorInfo{Prefix: ""},
			in:   "gj-65a8b3c0:12:100:99",
			want: "12:100:99",
		},
		{
			name: "empty",
			info: &CursorInfo{Prefix: "gj-123:"},
			in:   "   ",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCursorForSeek(tt.info, tt.in)
			if got != tt.want {
				t.Fatalf("normalizeCursorForSeek() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCursorSeekFilterDescWithID(t *testing.T) {
	info := &CursorInfo{
		SelID:  12,
		Prefix: "gj-65a8b3c0:",
		OrderBy: []CursorColumn{
			{Col: "price", Order: "desc"},
			{Col: "id", Order: "desc"},
		},
	}

	match := buildCursorSeekFilter(info, "gj-65a8b3c0:12:100.5:99")
	if match == nil {
		t.Fatalf("buildCursorSeekFilter() returned nil")
	}

	matchBody := match["$match"].(map[string]any)
	or := matchBody["$or"].([]map[string]any)
	if len(or) != 2 {
		t.Fatalf("unexpected OR condition count: %d", len(or))
	}

	// First clause: price < 100.5
	priceCmp := or[0]["price"].(map[string]any)
	if got := priceCmp["$lt"]; got != 100.5 {
		t.Fatalf("first clause price cmp = %v, want 100.5", got)
	}

	// Second clause: price = 100.5 AND _id < 99
	and := or[1]["$and"].([]map[string]any)
	if got := and[0]["price"]; got != 100.5 {
		t.Fatalf("second clause price eq = %v, want 100.5", got)
	}
	idCmp := and[1]["_id"].(map[string]any)
	if got := idCmp["$lt"]; got != int64(99) {
		t.Fatalf("second clause _id cmp = %v, want 99", got)
	}
}

func TestBuildCursorSeekFilterAsc(t *testing.T) {
	info := &CursorInfo{
		SelID:  4,
		Prefix: "gj-abc:",
		OrderBy: []CursorColumn{
			{Col: "id", Order: "asc"},
		},
	}

	match := buildCursorSeekFilter(info, "gj-abc:4:10")
	if match == nil {
		t.Fatalf("buildCursorSeekFilter() returned nil")
	}

	matchBody := match["$match"].(map[string]any)
	idCmp := matchBody["_id"].(map[string]any)
	if got := idCmp["$gt"]; got != int64(10) {
		t.Fatalf("asc _id cmp = %v, want 10", got)
	}
}

func TestBuildCursorSeekFilterInvalid(t *testing.T) {
	info := &CursorInfo{
		SelID:  4,
		Prefix: "gj-abc:",
		OrderBy: []CursorColumn{
			{Col: "id", Order: "asc"},
		},
	}

	if got := buildCursorSeekFilter(info, "gj-abc:"); got != nil {
		t.Fatalf("expected nil filter for invalid cursor, got: %#v", got)
	}
}
