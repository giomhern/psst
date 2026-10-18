package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/personal"
)

func TestDoctorReportsDatabaseCheck(t *testing.T) {
	for _, status := range []personal.DatabaseStatus{
		personal.DatabaseCompatible, personal.DatabaseIncompatible, personal.DatabaseMissing, personal.DatabaseDenied,
		personal.DatabaseNotRegular, personal.DatabaseUnavailable, personal.DatabaseUnsupported,
	} {
		t.Run(string(status), func(t *testing.T) {
			var output bytes.Buffer
			err := run(context.Background(), []string{"doctor"}, &output, func(context.Context) personal.DatabaseCheck {
				return personal.DatabaseCheck{Status: status, Detail: "safe diagnostic"}
			})
			if (err == nil) != (status == personal.DatabaseCompatible) {
				t.Fatalf("status %q: unexpected error %v", status, err)
			}
			for _, want := range []string{"Personal database: " + string(status), "safe diagnostic", "No message rows were queried", "Automation and message-format checks are not implemented"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("output missing %q: %s", want, output.String())
				}
			}
		})
	}
}

func TestOtherCommandsDoNotProbePersonal(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"unknown"}, {"doctor", "extra"}} {
		var output bytes.Buffer
		_ = run(context.Background(), args, &output, func(context.Context) personal.DatabaseCheck {
			t.Fatal("unexpected Personal access")
			return personal.DatabaseCheck{}
		})
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDoctorPreservesOutputFailure(t *testing.T) {
	want := errors.New("output unavailable")
	err := run(context.Background(), []string{"doctor"}, failingWriter{want}, func(context.Context) personal.DatabaseCheck {
		t.Fatal("probe should not run after initial output failed")
		return personal.DatabaseCheck{}
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestDoctorBoundsProbeAndPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	_ = run(ctx, []string{"doctor"}, &output, func(probeCtx context.Context) personal.DatabaseCheck {
		if !errors.Is(probeCtx.Err(), context.Canceled) {
			t.Fatal("caller cancellation was not propagated")
		}
		deadline, ok := probeCtx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("probe is missing its bounded deadline")
		}
		return personal.DatabaseCheck{Status: personal.DatabaseUnavailable, Detail: "canceled"}
	})
}

// Diagnostic tests inject only the doctor operation. Any unexpected chat
// access is blocked instead of falling through to the real Messages database.
func run(ctx context.Context, args []string, out io.Writer, check func(context.Context) personal.DatabaseCheck) error {
	return execute(ctx, args, out, dependencies{
		checkDatabase: check,
		openReader: func(context.Context) (conversationReader, error) {
			return nil, errors.New("unexpected Personal access")
		},
	})
}
