// Package cli implements the terminal client's command dispatch.
package cli

import (
	"fmt"
	"io"
	"runtime"
)

// Run executes one CLI command. The foundation has no Personal data access.
func Run(args []string, out io.Writer) error {
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
		_, err := fmt.Fprintf(out, "Platform: %s/%s\nGo: %s\nPersonal: requires macOS; database and permission checks are not implemented.\nSessions: not implemented.\nNo Messages data was accessed.\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
		return err
	default:
		return fmt.Errorf("unknown command %q (run psst --help)", args[0])
	}
}

func help(out io.Writer) error {
	_, err := fmt.Fprint(out, `Psst — terminal-native messaging

Usage:
  psst doctor    Show foundation diagnostics without accessing Messages
  psst --help    Show this help

Personal messaging, hosted Sessions, and the TUI are under development.
`)
	return err
}
