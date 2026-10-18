//go:build !darwin

package personal

import "context"

// CheckDatabase reports that Personal requires macOS without probing local files.
func CheckDatabase(_ context.Context) DatabaseCheck {
	return DatabaseCheck{Status: DatabaseUnsupported, Detail: "Personal requires macOS. Hosted Sessions are a separate capability."}
}
