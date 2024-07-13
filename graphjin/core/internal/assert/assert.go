package assert

import (
	"reflect"
	"testing"
)

// Equals compares two values
func Equals(t *testing.T, exp, got interface{}) {
	if !reflect.DeepEqual(exp, got) {
		t.Errorf("expected %v, got %v", exp, got)
	}
}

// Empty checks if a slice is empty
func Empty(t *testing.T, got interface{}) {
	val := reflect.ValueOf(got)
	if val.Kind() != reflect.Slice {
		t.Fatalf("not a slice: %v", got)
		return
	}

	if val.Len() != 0 {
		t.Errorf("expected empty slice, got %v", got)
		return
	}
}

// NoError checks if an error is nil
func NoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("no errror expected, got %s", err.Error())
	}
}

// NoErrorFatal checks if an error is nil and fails the test
func NoErrorFatal(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("no errror expected, got %s", err.Error())
	}
}
