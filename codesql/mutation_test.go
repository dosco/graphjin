//go:build cgo

package codesql

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestManagedMutationPreviewValidationFailures(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = `package main

func Value() int {
	return 1
}
`
	writeFile(t, filepath.Join(root, "main.go"), src)
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	start := int64(strings.Index(src, "func Value"))
	end := int64(strings.Index(src, "\n}\n") + len("\n}\n"))
	oldText := src[start:end]
	goodHash := hashBytes([]byte(src))

	cases := []struct {
		name string
		edit map[string]interface{}
		want string
	}{
		{
			name: "stale hash",
			edit: editInput("main.go", "stale", start, end, oldText, strings.Replace(oldText, "1", "2", 1)),
			want: "stale expected_hash",
		},
		{
			name: "old text mismatch",
			edit: editInput("main.go", goodHash, start, end, "not the old text", "replacement"),
			want: "old_text mismatch",
		},
		{
			name: "path traversal",
			edit: editInput("../main.go", goodHash, start, end, oldText, "replacement"),
			want: "escapes CodeSQL root",
		},
		{
			name: "overlap",
			edit: map[string]interface{}{
				"path":          "main.go",
				"expected_hash": goodHash,
				"replacements": []map[string]interface{}{
					{"start_byte": start, "end_byte": start + 10, "old_text": src[start : start+10], "new_text": "aaaaaaaaaa"},
					{"start_byte": start + 5, "end_byte": start + 15, "old_text": src[start+5 : start+15], "new_text": "bbbbbbbbbb"},
				},
			},
			want: "overlapping replacements",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := executeManagedMutationForTest(t, managed, MutationRoot{
				FieldName: "code_change_sets",
				Table:     "code_change_sets",
				Operation: "insert",
				Input: map[string]interface{}{
					"action": "preview",
					"edits":  []map[string]interface{}{tc.edit},
				},
			})
			status := out["code_change_sets"].(map[string]interface{})["status"].(string)
			if status != changeStatusConflict && status != changeStatusFailed {
				t.Fatalf("status = %q, want failure", status)
			}
			errors, _ := out["code_change_sets"].(map[string]interface{})["errors"].([]interface{})
			if len(errors) == 0 || !strings.Contains(errors[0].(string), tc.want) {
				t.Fatalf("errors = %#v, want %q", errors, tc.want)
			}
		})
	}
}

func TestManagedMutationCreateApplyAndValidation(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	out := previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":      "create",
		"path":    "nested/empty.go",
		"content": "",
		"mkdirs":  true,
	}})
	row := out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusPreviewed {
		t.Fatalf("preview output = %#v", row)
	}
	if diff := row["diff"].(string); !strings.Contains(diff, "+++ b/nested/empty.go") {
		t.Fatalf("create diff = %q", diff)
	}
	apply := applyChangeSetForTest(t, managed, int64(row["id"].(float64)))
	applied := apply["code_change_sets"].(map[string]interface{})
	if applied["status"] != changeStatusApplied {
		t.Fatalf("apply output = %#v", applied)
	}
	if data, err := os.ReadFile(filepath.Join(root, "nested", "empty.go")); err != nil || string(data) != "" {
		t.Fatalf("created file data = %q err=%v", data, err)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'nested/empty.go'`, 1)

	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":      "create",
		"path":    "nested/empty.go",
		"content": "package nested\n",
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "target already exists") {
		t.Fatalf("existing-target output = %#v", row)
	}

	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":      "create",
		"path":    "missing/file.go",
		"content": "package missing\n",
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusFailed || !firstErrorContains(row, "parent directory does not exist") {
		t.Fatalf("missing-parent output = %#v", row)
	}
}

func TestManagedMutationDeleteApplyAndValidation(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = "package main\n\nfunc Dead() {}\n"
	writeFile(t, filepath.Join(root, "dead.go"), src)
	writeFile(t, filepath.Join(root, "stale.go"), "package main\n\nfunc Stale() {}\n")
	if err := os.Mkdir(filepath.Join(root, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	out := previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "delete",
		"path":          "dead.go",
		"expected_hash": hashBytes([]byte(src)),
	}})
	row := out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusPreviewed {
		t.Fatalf("preview output = %#v", row)
	}
	if diff := row["diff"].(string); !strings.Contains(diff, "--- a/dead.go") || !strings.Contains(diff, "+++ /dev/null") {
		t.Fatalf("delete diff = %q", diff)
	}
	apply := applyChangeSetForTest(t, managed, int64(row["id"].(float64)))
	applied := apply["code_change_sets"].(map[string]interface{})
	if applied["status"] != changeStatusApplied {
		t.Fatalf("apply output = %#v", applied)
	}
	if _, err := os.Stat(filepath.Join(root, "dead.go")); !os.IsNotExist(err) {
		t.Fatalf("dead.go stat err = %v, want not exists", err)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'dead.go'`, 0)

	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "delete",
		"path":          "stale.go",
		"expected_hash": "stale",
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "stale expected_hash") {
		t.Fatalf("stale delete output = %#v", row)
	}

	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "delete",
		"path":          "dir",
		"expected_hash": "unused",
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusFailed || !firstErrorContains(row, "not a regular file") {
		t.Fatalf("directory delete output = %#v", row)
	}
}

func TestManagedMutationRenameApplyAndValidation(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = "package main\n\nfunc OldName() {}\n"
	writeFile(t, filepath.Join(root, "old.go"), src)
	writeFile(t, filepath.Join(root, "target.go"), "package main\n")
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	out := previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "rename",
		"path":          "old.go",
		"new_path":      "pkg/new.go",
		"expected_hash": hashBytes([]byte(src)),
		"mkdirs":        true,
	}})
	row := out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusPreviewed {
		t.Fatalf("preview output = %#v", row)
	}
	if diff := row["diff"].(string); !strings.Contains(diff, "--- a/old.go") || !strings.Contains(diff, "+++ b/pkg/new.go") {
		t.Fatalf("rename diff = %q", diff)
	}
	apply := applyChangeSetForTest(t, managed, int64(row["id"].(float64)))
	applied := apply["code_change_sets"].(map[string]interface{})
	if applied["status"] != changeStatusApplied {
		t.Fatalf("apply output = %#v", applied)
	}
	if _, err := os.Stat(filepath.Join(root, "old.go")); !os.IsNotExist(err) {
		t.Fatalf("old.go stat err = %v, want not exists", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "pkg", "new.go")); err != nil || string(data) != src {
		t.Fatalf("renamed file data = %q err=%v", data, err)
	}
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'old.go'`, 0)
	assertCount(t, managed.DB, `SELECT count(*) FROM code_files WHERE path = 'pkg/new.go'`, 1)

	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "rename",
		"path":          "target.go",
		"new_path":      "pkg/new.go",
		"expected_hash": hashBytes([]byte("package main\n")),
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "target already exists") {
		t.Fatalf("existing-target rename output = %#v", row)
	}
}

func TestManagedMutationLifecycleLockConflicts(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = "package main\n\nfunc Locked() {}\n"
	writeFile(t, filepath.Join(root, "main.go"), src)
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	lockOut := executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_locks",
		Table:     "code_locks",
		Operation: "insert",
		Input: map[string]interface{}{
			"action":     "acquire",
			"path":       "main.go",
			"owner":      "test",
			"whole_file": true,
		},
	})
	lock := lockOut["code_locks"].(map[string]interface{})
	token := lock["lease_token"].(string)

	out := previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":            "delete",
		"path":          "main.go",
		"expected_hash": hashBytes([]byte(src)),
	}})
	row := out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "active lock") {
		t.Fatalf("locked delete output = %#v", row)
	}

	out = executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_change_sets",
		Table:     "code_change_sets",
		Operation: "insert",
		Input: map[string]interface{}{
			"action":      "preview",
			"lease_token": token,
			"edits": []map[string]interface{}{{
				"op":            "delete",
				"path":          "main.go",
				"expected_hash": hashBytes([]byte(src)),
			}},
		},
	})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusPreviewed {
		t.Fatalf("owned-lock delete output = %#v", row)
	}

	targetLockOut := executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_locks",
		Table:     "code_locks",
		Operation: "insert",
		Input: map[string]interface{}{
			"action":     "acquire",
			"path":       "reserved.go",
			"owner":      "test",
			"whole_file": true,
		},
	})
	targetLock := targetLockOut["code_locks"].(map[string]interface{})
	out = previewEditsForTest(t, managed, []map[string]interface{}{{
		"op":      "create",
		"path":    "reserved.go",
		"content": "package main\n",
	}})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "active lock") {
		t.Fatalf("reserved create output = %#v", row)
	}

	out = executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_change_sets",
		Table:     "code_change_sets",
		Operation: "insert",
		Input: map[string]interface{}{
			"action":      "preview",
			"lease_token": targetLock["lease_token"].(string),
			"edits": []map[string]interface{}{{
				"op":      "create",
				"path":    "reserved.go",
				"content": "package main\n",
			}},
		},
	})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusPreviewed {
		t.Fatalf("owned-reservation create output = %#v", row)
	}
}

func TestManagedMutationMixedLifecyclePathConflicts(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = "package main\n\nfunc Value() int { return 1 }\n"
	writeFile(t, filepath.Join(root, "main.go"), src)
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	out := previewEditsForTest(t, managed, []map[string]interface{}{
		{"op": "create", "path": "dup.go", "content": "package main\n"},
		{"op": "create", "path": "dup.go", "content": "package main\n"},
	})
	row := out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "internal path conflict") {
		t.Fatalf("duplicate create output = %#v", row)
	}

	start := int64(strings.Index(src, "return 1"))
	end := start + int64(len("return 1"))
	out = previewEditsForTest(t, managed, []map[string]interface{}{
		editInput("main.go", hashBytes([]byte(src)), start, end, "return 1", "return 2"),
		{"op": "delete", "path": "main.go", "expected_hash": hashBytes([]byte(src))},
	})
	row = out["code_change_sets"].(map[string]interface{})
	if row["status"] != changeStatusConflict || !firstErrorContains(row, "internal path conflict") {
		t.Fatalf("replace/delete output = %#v", row)
	}
}

func TestManagedMutationConcurrentOverlappingApplyOneWins(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "codesql")
	const src = `package main

func Value() int {
	return 1
}
`
	writeFile(t, filepath.Join(root, "main.go"), src)
	managed, _, err := OpenManaged(context.Background(), Options{Name: "code", Root: root, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer managed.Close()

	start := int64(strings.Index(src, "return 1"))
	end := start + int64(len("return 1"))
	oldText := src[start:end]
	goodHash := hashBytes([]byte(src))
	ids := make([]int64, 2)
	for i, next := range []string{"return 2", "return 3"} {
		out := executeManagedMutationForTest(t, managed, MutationRoot{
			FieldName: "code_change_sets",
			Table:     "code_change_sets",
			Operation: "insert",
			Input: map[string]interface{}{
				"action": "preview",
				"edits": []map[string]interface{}{
					editInput("main.go", goodHash, start, end, oldText, next),
				},
			},
		})
		row := out["code_change_sets"].(map[string]interface{})
		if row["status"] != changeStatusPreviewed {
			t.Fatalf("preview %d status = %#v", i, row)
		}
		ids[i] = int64(row["id"].(float64))
	}

	var wg sync.WaitGroup
	statuses := make([]string, 2)
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			data, err := managed.ExecuteManagedMutation(context.Background(), MutationRequest{
				Database:  "code",
				Operation: "update",
				Roots: []MutationRoot{{
					FieldName: "code_change_sets",
					Table:     "code_change_sets",
					Operation: "update",
					Input: map[string]interface{}{
						"id":     id,
						"action": "apply",
					},
				}},
			})
			if err != nil {
				statuses[i] = "error: " + err.Error()
				return
			}
			var out map[string]interface{}
			if err := json.Unmarshal(data, &out); err != nil {
				statuses[i] = "error: " + err.Error()
				return
			}
			statuses[i] = out["code_change_sets"].(map[string]interface{})["status"].(string)
		}(i, id)
	}
	wg.Wait()

	applied, conflicted := 0, 0
	for _, status := range statuses {
		switch status {
		case changeStatusApplied:
			applied++
		case changeStatusConflict:
			conflicted++
		}
	}
	if applied != 1 || conflicted != 1 {
		t.Fatalf("statuses = %#v, want one applied and one conflict", statuses)
	}
}

func previewEditsForTest(t *testing.T, managed *Managed, edits []map[string]interface{}) map[string]interface{} {
	t.Helper()
	return executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_change_sets",
		Table:     "code_change_sets",
		Operation: "insert",
		Input: map[string]interface{}{
			"action": "preview",
			"edits":  edits,
		},
	})
}

func applyChangeSetForTest(t *testing.T, managed *Managed, id int64) map[string]interface{} {
	t.Helper()
	return executeManagedMutationForTest(t, managed, MutationRoot{
		FieldName: "code_change_sets",
		Table:     "code_change_sets",
		Operation: "update",
		Input: map[string]interface{}{
			"id":     id,
			"action": "apply",
		},
	})
}

func firstErrorContains(row map[string]interface{}, want string) bool {
	errors, _ := row["errors"].([]interface{})
	return len(errors) != 0 && strings.Contains(errors[0].(string), want)
}

func editInput(path, hash string, start, end int64, oldText, newText string) map[string]interface{} {
	return map[string]interface{}{
		"path":          path,
		"expected_hash": hash,
		"replacements": []map[string]interface{}{
			{
				"start_byte": start,
				"end_byte":   end,
				"old_text":   oldText,
				"new_text":   newText,
			},
		},
	}
}

func executeManagedMutationForTest(t *testing.T, managed *Managed, root MutationRoot) map[string]interface{} {
	t.Helper()
	data, err := managed.ExecuteManagedMutation(context.Background(), MutationRequest{
		Database:  "code",
		Operation: root.Operation,
		Roots:     []MutationRoot{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode managed response: %v\n%s", err, data)
	}
	return out
}
