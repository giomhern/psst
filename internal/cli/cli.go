// Package cli implements the terminal client's command dispatch.
package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/giomhern/psst/internal/personal"
)

// Run executes one CLI command. Only doctor probes Personal filesystem access.
func Run(args []string, out io.Writer) error {
	return run(args, out, personal.CheckDatabase)
}

// Inject the one operation doctor needs; tests never inspect real Messages data.
func run(args []string, out io.Writer, checkDatabase func() personal.DatabaseCheck) error {
	if len(args) == 0 {
		return help(out)
	}
	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return fmt.Errorf("help takes no arguments")
		}
		return help(out)
	case "doctor":
		if len(args) != 1 {
			return fmt.Errorf("doctor takes no arguments")
		}
		return doctor(out, checkDatabase)
	default:
		return fmt.Errorf("unknown command %q (run psst --help)", args[0])
	}
}

func help(out io.Writer) error {
	_, err := fmt.Fprint(out, `Psst — terminal-native messaging

Usage:
  psst doctor    Check platform and read-only Messages database access
  psst --help    Show this help

Personal messaging, hosted Sessions, and the TUI are under development.
`)
	return err
}

func doctor(out io.Writer, checkDatabase func() personal.DatabaseCheck) error {
	if _, err := fmt.Fprintf(out, "Platform: %s/%s\nGo: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version()); err != nil {
		return err
	}
	check := checkDatabase()
	if _, err := fmt.Fprintf(out, `Personal database: %s
%s
Schema, WAL, and automation checks are not implemented.
Sessions: not implemented.
No message content was read or uploaded; no database writes were performed.
`, check.Status, check.Detail); err != nil {
		return err
	}
	if check.Status != personal.DatabaseAccessible {
		return errors.New("personal database access check did not pass")
	}
	return nil
}
