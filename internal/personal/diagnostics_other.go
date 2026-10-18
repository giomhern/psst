//go:build !darwin

package personal

import (
	"context"
	"errors"
)

// CheckDatabase reports that Personal requires macOS without probing local files.
func CheckDatabase(_ context.Context) DatabaseCheck {
	return DatabaseCheck{Status: DatabaseUnsupported, Detail: "Personal requires macOS. Hosted Sessions are a separate capability."}
}

// Open reports that local Messages requires macOS without probing local files.
func Open(_ context.Context) (*Reader, error) {
	return nil, errors.New("personal messaging requires macOS; hosted Sessions are a separate capability")
}
