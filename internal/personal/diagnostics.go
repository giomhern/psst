// Package personal provides local macOS Messages capabilities.
package personal

import (
	"errors"
	"io/fs"
)

const databasePath = "Library/Messages/chat.db"

// DatabaseStatus describes the outcome of a filesystem access check only.
type DatabaseStatus string

const (
	// DatabaseAccessible means a regular file could be opened read-only.
	DatabaseAccessible DatabaseStatus = "accessible"
	// DatabaseMissing means the expected file or a parent directory is absent.
	DatabaseMissing DatabaseStatus = "missing"
	// DatabaseDenied means filesystem or macOS privacy permissions denied access.
	DatabaseDenied DatabaseStatus = "access_denied"
	// DatabaseNotRegular means the expected path is not a regular file.
	DatabaseNotRegular DatabaseStatus = "not_regular"
	// DatabaseUnavailable means another filesystem or home-directory error occurred.
	DatabaseUnavailable DatabaseStatus = "unavailable"
	// DatabaseUnsupported means Personal is unavailable on this operating system.
	DatabaseUnsupported DatabaseStatus = "unsupported"
)

// DatabaseCheck is a content-free diagnostic. Detail never includes raw errors,
// absolute paths, file contents, or evidence of SQLite schema compatibility.
type DatabaseCheck struct {
	Status DatabaseStatus
	Detail string
}

func checkDatabase(filesystem fs.FS) DatabaseCheck {
	// Inspect metadata first so obvious directories/devices are not opened.
	info, err := fs.Stat(filesystem, databasePath)
	if err != nil {
		return databaseError(err)
	}
	if !info.Mode().IsRegular() {
		return notRegular()
	}
	file, err := filesystem.Open(databasePath)
	if err != nil {
		return databaseError(err)
	}
	// Inspect the opened file too: the path may have changed since the first stat.
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return databaseError(statErr)
	}
	if closeErr != nil {
		return databaseError(closeErr)
	}
	if !info.Mode().IsRegular() {
		return notRegular()
	}
	return DatabaseCheck{
		Status: DatabaseAccessible,
		Detail: "Regular file opened read-only. SQLite schema and WAL access are not checked yet.",
	}
}

func notRegular() DatabaseCheck {
	return DatabaseCheck{Status: DatabaseNotRegular, Detail: "Expected a regular file at ~/Library/Messages/chat.db. No content was read."}
}

func databaseError(err error) DatabaseCheck {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DatabaseCheck{Status: DatabaseMissing, Detail: "No database found at ~/Library/Messages/chat.db. Open Messages and confirm local setup; psst will not create it."}
	case errors.Is(err, fs.ErrPermission):
		return DatabaseCheck{Status: DatabaseDenied, Detail: "Access denied by filesystem permissions or macOS privacy controls. Review System Settings > Privacy & Security > Full Disk Access for the terminal/app running psst, then retry. psst does not change permissions."}
	default:
		return DatabaseCheck{Status: DatabaseUnavailable, Detail: "Could not complete the database access check. Check the home directory and local filesystem, then retry."}
	}
}
