package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	ax "github.com/ax-llm/ax/packages/go"
)

// A result up to 100,000 characters reaches actor code whole; a larger one
// becomes a saved reference with a 1,500-character preview. Results go to
// code, not to the model context, so one step has no combined limit.
const (
	InlineResultChars  = 100_000
	PreviewChars       = 1_500
	MaxStoredResultLen = 16 << 20
)

func savedReference(id int, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("saved:%d:%s", id, hex.EncodeToString(sum[:]))
}

func runePrefix(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func safePrefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// visibleOperationResult keeps large tool data in the run's authoritative
// operation record. The actor gets a bounded hint and can explicitly inspect
// the full result through its run-local callable when a field is needed.
func visibleOperationResult(id int, raw json.RawMessage, decoded ax.Value) ax.Value {
	if utf8.RuneCount(raw) <= InlineResultChars {
		return decoded
	}
	return ax.Object(
		"reference", savedReference(id, raw),
		"result_bytes", len(raw),
		"result_preview", runePrefix(string(raw), PreviewChars),
		"retrieve", fmt.Sprintf("harnessSavedOperation(%d)", id),
		"is_error", toolResultFailed(raw),
	)
}

// savedOperationValue returns one operation from a run-local snapshot. Model-
// chosen IDs have no filesystem or tenant access; callers supply only the
// operations admitted to this runtime.
func savedOperationValue(operations []SavedOperation, value ax.Value) (ax.Value, error) {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || number < 1 || number > float64(len(operations)) || math.Trunc(number) != number {
		return nil, fmt.Errorf("saved operation unavailable")
	}
	op := operations[int(number)-1]
	if op.ID != int(number) || !op.Finished {
		return nil, fmt.Errorf("saved operation unavailable")
	}
	encoded, err := json.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("saved operation unavailable")
	}
	var result ax.Value
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("saved operation unavailable")
	}
	return result, nil
}
