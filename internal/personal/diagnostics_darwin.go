package personal

import (
	"os"
	"path/filepath"
)

// CheckDatabase checks the current user's Messages database metadata and whether
// it can be opened read-only. It never reads content or changes permissions.
func CheckDatabase() DatabaseCheck {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return DatabaseCheck{Status: DatabaseUnavailable, Detail: "Could not locate an absolute user home directory. Check the environment used to launch psst."}
	}
	return checkDatabase(os.DirFS(home))
}
