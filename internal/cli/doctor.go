package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/giomhern/psst/internal/personal"
	"io"
	"runtime"
	"time"
)

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
