package jsn

import (
	"bytes"
	"encoding/json"
)

func Tree(v []byte) (map[string]json.RawMessage, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(v))
	array := false

	// read open bracket

	for i := range v {
		if v[i] != ' ' {
			array = (v[i] == '[')
			break
		}
	}

	if array {
		if _, err := dec.Token(); err != nil {
			return nil, false, err
		}
	}

	// while the array contains values
	var m map[string]json.RawMessage

	// decode an array value (Message)
	err := dec.Decode(&m)
	if err != nil {
		return nil, false, err
	}

	return m, array, nil
}
