package personal

import (
	"context"
	"errors"
	"github.com/giomhern/psst/internal/personal/contacts"
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

// Open opens the current user's Messages database read-only. It does not inspect
// or list conversation data until the caller invokes a reader method.
func Open(ctx context.Context) (*Reader, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, errors.New("could not locate an absolute user home directory")
	}
	reader, err := openReader(ctx, filepath.Join(home, filepath.FromSlash(databasePath)))
	if err != nil {
		return nil, err
	}
	reader.names = contacts.New()
	return reader, nil
}
