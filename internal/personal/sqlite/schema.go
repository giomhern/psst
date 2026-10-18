package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Schema describes the known minimum structure needed for planned basic chat and
// message queries. An empty Missing list does not validate timestamp encodings,
// attributed message bodies, delivery status, or all macOS schema versions.
type Schema struct{ Missing []string }

type tableSpec struct {
	name    string
	columns []string
	rowID   bool
}

// InspectSchema reads only SQLite metadata in a consistent read transaction.
// Missing identifiers come from our fixed allowlist, never from arbitrary schema
// names or message rows. The caller's context bounds waiting and query execution.
func (r *Reader) InspectSchema(ctx context.Context) (Schema, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Schema{}, fmt.Errorf("begin schema inspection: %w", err)
	}
	defer tx.Rollback()
	report, err := inspectSchema(ctx, tx)
	if err != nil {
		return Schema{}, err
	}

	if err := tx.Commit(); err != nil {
		return Schema{}, fmt.Errorf("finish schema inspection: %w", err)
	}
	return report, nil
}

func columnNames(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	// Bound metadata work and fail closed for unusually wide tables.
	rows, err := tx.QueryContext(ctx, "SELECT lower(name) FROM pragma_table_info(?) LIMIT 513", table)
	if err != nil {
		return nil, fmt.Errorf("inspect column metadata: %w", err)
	}
	defer rows.Close()
	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("decode column metadata: %w", err)
		}
		names[name] = true
		if len(names) > 512 {
			return nil, errors.New("table exceeds supported metadata column limit")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate column metadata: %w", err)
	}
	return names, nil
}

func inspectSchema(ctx context.Context, tx *sql.Tx) (Schema, error) {
	required := []tableSpec{
		{"chat", []string{"guid"}, true},
		{"message", []string{"guid", "text", "date", "is_from_me", "handle_id"}, true},
		{"handle", []string{"id"}, true},
		{"chat_message_join", []string{"chat_id", "message_id"}, false},
		{"chat_handle_join", []string{"chat_id", "handle_id"}, false},
	}
	report := Schema{}
	for _, spec := range required {
		var exists bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = ? COLLATE NOCASE)", spec.name).Scan(&exists)
		if err != nil {
			return Schema{}, fmt.Errorf("inspect table metadata: %w", err)
		}
		if !exists {
			report.Missing = append(report.Missing, spec.name)
			continue
		}
		columns, err := columnNames(ctx, tx, spec.name)
		if err != nil {
			return Schema{}, err
		}
		for _, name := range spec.columns {
			if !columns[name] {
				report.Missing = append(report.Missing, spec.name+"."+name)
			}
		}
		if spec.rowID {
			var withoutRowID bool
			err := tx.QueryRowContext(ctx, "SELECT wr FROM pragma_table_list WHERE schema = 'main' AND name = ? COLLATE NOCASE", spec.name).Scan(&withoutRowID)
			if err != nil {
				return Schema{}, fmt.Errorf("inspect rowid metadata: %w", err)
			}
			if withoutRowID {
				report.Missing = append(report.Missing, spec.name+".rowid")
			}
		}
	}
	return report, nil
}
