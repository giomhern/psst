//go:build !darwin

package personal

import "testing"

func TestCheckDatabaseUnsupported(t *testing.T) {
	if got := CheckDatabase(); got.Status != DatabaseUnsupported {
		t.Fatalf("status = %q, want unsupported", got.Status)
	}
}
