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
