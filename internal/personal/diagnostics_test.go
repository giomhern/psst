package personal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

func TestCheckDatabaseWithSyntheticFiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, string)
		want  DatabaseStatus
	}{
		{"missing parent", func(*testing.T, string) {}, DatabaseMissing},
		{"missing database", func(t *testing.T, home string) {
			if err := os.MkdirAll(filepath.Join(home, "Library", "Messages"), 0700); err != nil {
				t.Fatal(err)
			}
		}, DatabaseMissing},
		{"directory", func(t *testing.T, home string) {
			if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(databasePath)), 0700); err != nil {
				t.Fatal(err)
			}
		}, DatabaseNotRegular},
		{"readable synthetic file", func(t *testing.T, home string) {
			if err := os.MkdirAll(filepath.Join(home, "Library", "Messages"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"", "-wal", "-shm"} {
				if err := os.WriteFile(filepath.Join(home, filepath.FromSlash(databasePath))+suffix, []byte("synthetic content"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}, DatabaseAccessible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)
			got := checkDatabase(os.DirFS(home))
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
			if strings.Contains(got.Detail, home) {
				t.Fatal("diagnostic leaked absolute path")
			}
			if tc.want == DatabaseAccessible {
				for _, suffix := range []string{"", "-wal", "-shm"} {
					content, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(databasePath)) + suffix)
					if err != nil {
						t.Fatal(err)
					}
					if string(content) != "synthetic content" {
						t.Fatal("fixture was changed")
					}
				}
			}
			if tc.want == DatabaseMissing {
				if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(databasePath))); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("missing database was created or stat failed: %v", err)
				}
			}
		})
	}
}

// Metadata and open failures are independent so permission-denial tests do not
// depend on the test runner's privileges.
type diagnosticFS struct {
	fs.FS
	openErr     error
	metadataErr error
	closeErr    error
	statErr     error
	closed      bool
}

func (f *diagnosticFS) Stat(name string) (fs.FileInfo, error) {
	if f.metadataErr != nil {
		return nil, &fs.PathError{Op: "stat", Path: "private-absolute-path", Err: f.metadataErr}
	}
	return fs.Stat(f.FS, name)
}
func (f *diagnosticFS) Open(name string) (fs.File, error) {
	if f.openErr != nil {
		return nil, &fs.PathError{Op: "open", Path: "private-absolute-path", Err: f.openErr}
	}
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &diagnosticFile{File: file, owner: f}, nil
}

type diagnosticFile struct {
	fs.File
	owner *diagnosticFS
}

func (f *diagnosticFile) Read([]byte) (int, error) { panic("diagnostic must not read content") }
func (f *diagnosticFile) Stat() (fs.FileInfo, error) {
	if f.owner.statErr != nil {
		return nil, f.owner.statErr
	}
	return f.File.Stat()
}
func (f *diagnosticFile) Close() error {
	f.owner.closed = true
	return errors.Join(f.File.Close(), f.owner.closeErr)
}

func TestCheckDatabaseDoesNotReadAndClosesFiles(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		openErr, statErr, closeErr, metadataErr error
		want                                    DatabaseStatus
	}{
		{name: "read-only success", want: DatabaseAccessible},
		{name: "metadata denied", metadataErr: syscall.EPERM, want: DatabaseDenied},
		{name: "filesystem denial", openErr: syscall.EACCES, want: DatabaseDenied},
		{name: "privacy-style denial", openErr: syscall.EPERM, want: DatabaseDenied},
		{name: "removed after stat", openErr: fs.ErrNotExist, want: DatabaseMissing},
		{name: "unexpected failure", openErr: errors.New("private error text"), want: DatabaseUnavailable},
		{name: "opened-file stat failure", statErr: errors.New("private error text"), want: DatabaseUnavailable},
		{name: "close failure", closeErr: errors.New("private error text"), want: DatabaseUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filesystem := &diagnosticFS{
				FS:      fstest.MapFS{databasePath: {Data: []byte("private fixture text"), Mode: 0600}},
				openErr: tc.openErr, statErr: tc.statErr, closeErr: tc.closeErr, metadataErr: tc.metadataErr,
			}
			got := checkDatabase(filesystem)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
			if tc.metadataErr == nil && tc.openErr == nil && !filesystem.closed {
				t.Fatal("opened file was not closed")
			}
			if strings.Contains(got.Detail, "private") {
				t.Fatalf("diagnostic leaked data: %s", got.Detail)
			}
		})
	}
}
