package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	personaldb "github.com/giomhern/psst/internal/personal/sqlite"
)

func TestReadCursorFlowWithSyntheticSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-pages.db")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	schema, err := os.ReadFile("../personal/sqlite/testdata/baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(string(schema) + `DELETE FROM message;
        INSERT INTO chat(ROWID,guid) VALUES(1,'synthetic');
        INSERT INTO message(ROWID,date,text) VALUES(1,788918400,'one'),(2,788918400,'two'),(3,788918400,'three'),(4,NULL,'unknown');
        INSERT INTO chat_message_join VALUES(1,1),(1,2),(1,3),(1,4);`); err != nil {
		t.Fatal(err)
	}
	deps := dependencies{openMessages: func(ctx context.Context) (messageReader, error) { return personaldb.Open(ctx, path) }}
	var token string
	var ids []string
	// Varying page sizes must not affect the exclusive boundary.
	for _, limit := range []string{"1", "2", "1"} {
		args := []string{"personal", "read", "1", "--limit", limit}
		if token != "" {
			args = append(args, "--before", token)
		}
		var out bytes.Buffer
		if err := execute(context.Background(), args, &out, deps); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(out.String(), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && (fields[0] == "1" || fields[0] == "2" || fields[0] == "3" || fields[0] == "4") {
				ids = append(ids, fields[0])
			}
		}
		_, tail, more := strings.Cut(out.String(), "Next --before: ")
		token = strings.TrimSpace(tail)
		if !more && !strings.Contains(out.String(), "End of locally synchronized history.") {
			t.Fatal("missing history termination")
		}
	}
	if !reflect.DeepEqual(ids, []string{"3", "1", "2", "4"}) || token != "" {
		t.Fatalf("paginated output IDs = %v", ids)
	}
}
