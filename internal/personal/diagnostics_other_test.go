//go:build !darwin

package personal

import (
	"context"
	"testing"
)

func TestCheckDatabaseUnsupported(t *testing.T) {
	if got := CheckDatabase(context.Background()); got.Status != DatabaseUnsupported {
		t.Fatalf("status = %q, want unsupported", got.Status)
	}
}

func TestOpenUnsupported(t *testing.T) {
	reader, err := Open(context.Background())
	if err == nil || reader != nil {
		t.Fatalf("reader = %v, error = %v", reader, err)
	}
}
