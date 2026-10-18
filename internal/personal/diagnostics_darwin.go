package personal

import (
	"context"
	"os"
	"path/filepath"
)

// CheckDatabase checks file access and the basic SQLite schema read-only. It does
// not query message rows, change permissions, migrate, or repair the database.
func CheckDatabase(ctx context.Context) DatabaseCheck {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return DatabaseCheck{Status: DatabaseUnavailable, Detail: "Could not locate an absolute user home directory. Check the environment used to launch psst."}
	}
	fileCheck := checkDatabase(os.DirFS(home))
	if fileCheck.Status != DatabaseAccessible {
		return fileCheck
	}
	return checkSchema(ctx, filepath.Join(home, filepath.FromSlash(databasePath)))
}
