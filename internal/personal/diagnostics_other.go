//go:build !darwin

package personal

// CheckDatabase reports that Personal requires macOS without probing local files.
func CheckDatabase() DatabaseCheck {
	return DatabaseCheck{Status: DatabaseUnsupported, Detail: "Personal requires macOS. Hosted Sessions are a separate capability."}
}
