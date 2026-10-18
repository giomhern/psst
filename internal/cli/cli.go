// Package cli implements the terminal client's command dispatch.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/giomhern/psst/internal/personal"
)

// Run executes one CLI command. Only doctor probes Personal access and schema.
func Run(ctx context.Context, args []string, out io.Writer) error {
	return run(ctx, args, out, personal.CheckDatabase)
}

// Inject the one operation doctor needs; tests never inspect real Messages data.
func run(ctx context.Context, args []string, out io.Writer, checkDatabase func(context.Context) personal.DatabaseCheck) error {
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
		return doctor(ctx, out, checkDatabase)
	default:
		return fmt.Errorf("unknown command %q (run psst --help)", args[0])
	}
}

func help(out io.Writer) error {
	_, err := fmt.Fprint(out, `Psst — terminal-native messaging

Usage:
  psst doctor    Check read-only Messages access and SQLite schema
  psst --help    Show this help

Personal messaging, hosted Sessions, and the TUI are under development.
`)
	return err
}

func doctor(ctx context.Context, out io.Writer, checkDatabase func(context.Context) personal.DatabaseCheck) error {
	if _, err := fmt.Fprintf(out, "Platform: %s/%s\nGo: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version()); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	check := checkDatabase(probeCtx)
	if _, err := fmt.Fprintf(out, `Personal database: %s
%s
Automation and message-format checks are not implemented.
Sessions: not implemented.
No message rows were queried or uploaded; no database writes were requested.
`, check.Status, check.Detail); err != nil {
		return err
	}
	if check.Status != personal.DatabaseCompatible {
		return errors.New("personal database diagnostic did not pass")
	}
	return nil
}
