package personal

import (
	"context"
	"errors"
	"strings"

	"github.com/giomhern/psst/internal/personal/sqlite"
)

func checkSchema(ctx context.Context, path string) DatabaseCheck {
	reader, err := sqlite.Open(ctx, path)
	if err != nil {
		return schemaError(ctx, err)
	}
	schema, inspectErr := reader.InspectSchema(ctx)
	closeErr := reader.Close()
	if err := errors.Join(inspectErr, closeErr); err != nil {
		return schemaError(ctx, err)
	}
	if len(schema.Missing) != 0 {
		return DatabaseCheck{Status: DatabaseIncompatible, Detail: "Unsupported schema; missing required tables/columns: " + strings.Join(schema.Missing, ", ") + ". Personal queries remain disabled."}
	}
	return DatabaseCheck{Status: DatabaseCompatible, Detail: "Read-only SQLite inspection passed the basic schema baseline. Timestamp encoding and rich message formats are not validated yet."}
}

func schemaError(ctx context.Context, err error) DatabaseCheck {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return DatabaseCheck{Status: DatabaseUnavailable, Detail: "SQLite schema check was canceled or timed out. Retry when the database is available."}
	}
	// Driver errors can include file paths or schema text. Keep them private.
	return DatabaseCheck{Status: DatabaseUnavailable, Detail: "Could not inspect SQLite metadata read-only. The file may be invalid, busy, inaccessible, or missing readable WAL sidecars. No repair or migration was attempted."}
}
