package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/giomhern/psst/internal/personal"
)

func TestDoctorReportsDatabaseCheck(t *testing.T) {
	for _, status := range []personal.DatabaseStatus{
		personal.DatabaseAccessible, personal.DatabaseMissing, personal.DatabaseDenied,
		personal.DatabaseNotRegular, personal.DatabaseUnavailable, personal.DatabaseUnsupported,
	} {
		t.Run(string(status), func(t *testing.T) {
			var output bytes.Buffer
			err := run([]string{"doctor"}, &output, func() personal.DatabaseCheck {
				return personal.DatabaseCheck{Status: status, Detail: "safe diagnostic"}
			})
			if (err == nil) != (status == personal.DatabaseAccessible) {
				t.Fatalf("status %q: unexpected error %v", status, err)
			}
			for _, want := range []string{"Personal database: " + string(status), "safe diagnostic", "No message content was read", "Schema, WAL, and automation checks are not implemented"} {
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
		_ = run(args, &output, func() personal.DatabaseCheck {
			t.Fatal("unexpected Personal access")
			return personal.DatabaseCheck{}
		})
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDoctorPreservesOutputFailure(t *testing.T) {
	want := errors.New("output unavailable")
	err := run([]string{"doctor"}, failingWriter{want}, func() personal.DatabaseCheck {
		t.Fatal("probe should not run after initial output failed")
		return personal.DatabaseCheck{}
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
