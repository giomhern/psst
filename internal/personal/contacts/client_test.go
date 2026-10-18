package contacts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLookupBatchesOnlyRequestedIdentifiers(t *testing.T) {
	calls := 0
	client := &Client{run: func(_ context.Context, action string, input []byte) ([]byte, error) {
		calls++
		if action != "lookup" {
			t.Fatalf("unexpected action = %s", action)
		}
		var request struct {
			Version int      `json:"v"`
			Handles []string `json:"handles"`
		}
		if err := json.Unmarshal(input, &request); err != nil {
			t.Fatal(err)
		}
		if request.Version != 1 || !reflect.DeepEqual(request.Handles, []string{"+15550000001", "a@example.test"}) {
			t.Fatalf("request = %+v", request)
		}
		return []byte(`{"v":1,"status":"authorized","names":{"+15550000001":"Alex","a@example.test":"Sam","unrequested-private":"ignored"}}`), nil
	}}
	got, err := client.Lookup(context.Background(), []string{" +15550000001 ", "+15550000001", "a@example.test", "", strings.Repeat("x", 321)})
	if err != nil || calls != 1 || !reflect.DeepEqual(got, map[string]string{"+15550000001": "Alex", "a@example.test": "Sam"}) {
		t.Fatalf("result = %v, %v", got, err)
	}
}

func TestLookupUnavailableAndMalformedResponses(t *testing.T) {
	for _, raw := range []string{
		`{"v":1,"status":"permission_required"}`, `{"v":1,"status":"denied"}`,
		`{"v":1,"status":"restricted"}`, `{"v":2,"status":"authorized"}`,
		`{"v":1,"status":"private-unknown"}`, `private-invalid-json`,
		strings.Repeat("x", (2<<20)+1),
	} {
		client := &Client{run: func(context.Context, string, []byte) ([]byte, error) { return []byte(raw), nil }}
		got, err := client.Lookup(context.Background(), []string{"+15550000001"})
		if got != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unexpected result = %v, %v", got, err)
		}
		if strings.Contains(err.Error(), "private") {
			t.Fatal("error leaked helper output")
		}
	}
	for _, client := range []*Client{{}, {unsupported: true}} {
		if _, err := client.Lookup(context.Background(), []string{"a@example.test"}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unavailable = %v", err)
		}
	}
}

func TestLookupBoundsRequestsAndNames(t *testing.T) {
	client := &Client{run: func(_ context.Context, _ string, input []byte) ([]byte, error) {
		var request struct {
			Handles []string `json:"handles"`
		}
		if err := json.Unmarshal(input, &request); err != nil {
			t.Fatal(err)
		}
		if len(request.Handles) > 500 || len(input) > 1<<20 {
			t.Fatal("unbounded request")
		}
		raw, err := json.Marshal(response{Version: 1, Status: Limited, Names: map[string]string{
			"a@example.test": strings.Repeat("猫", 201), "b@example.test": "  Bea  ",
		}})
		return raw, err
	}}
	handles := []string{"a@example.test", "b@example.test"}
	handles = append(handles, make([]string, 1000)...)
	got, err := client.Lookup(context.Background(), handles)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"b@example.test": "Bea"}) {
		t.Fatalf("names = %v, %v", got, err)
	}
}

func TestOnlyExplicitAuthorizationUsesAuthorizeAction(t *testing.T) {
	var actions []string
	client := &Client{run: func(_ context.Context, action string, _ []byte) ([]byte, error) {
		actions = append(actions, action)
		return []byte(`{"v":1,"status":"permission_required"}`), nil
	}}
	_, _ = client.Access(context.Background(), false)
	_, _ = client.Lookup(context.Background(), []string{"a@example.test"})
	_, _ = client.Access(context.Background(), true)
	if !reflect.DeepEqual(actions, []string{"status", "lookup", "authorize"}) {
		t.Fatalf("actions = %v", actions)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Access(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if len(actions) != 3 {
		t.Fatal("canceled call launched helper")
	}
}

func TestProcessRunnerUsesExecutableWithoutShellAndRedactsErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper with spaces;not-a-shell")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"v\":1,\"status\":\"authorized\",\"names\":{}}'\nprintf '%s' 'private stderr' >&2\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := commandRunner(path)(context.Background(), "lookup", []byte("synthetic request"))
	if err != nil || strings.Contains(string(output), "private") {
		t.Fatalf("runner = %q, %v", output, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' 'private stderr' >&2\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := commandRunner(path)(context.Background(), "status", nil); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), path) {
		t.Fatalf("failure = %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := commandRunner(path)(ctx, "status", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
}

func TestLimitedOutputCannotBypassWriteLimit(t *testing.T) {
	buffer := &limitedBuffer{limit: 8}
	if _, err := io.Copy(buffer, strings.NewReader(strings.Repeat("x", 9))); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("limit error = %v", err)
	}
	if len(buffer.Bytes()) > 8 {
		t.Fatal("output exceeded cap")
	}
}

func TestExplicitHelperConfiguration(t *testing.T) {
	t.Setenv("PSST_CONTACTS_HELPER", "off")
	if helperPath() != "" {
		t.Fatal("opt-out ignored")
	}
	t.Setenv("PSST_CONTACTS_HELPER", "relative-path")
	if helperPath() != "" {
		t.Fatal("relative helper accepted")
	}
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PSST_CONTACTS_HELPER", path)
	if helperPath() != path {
		t.Fatal("explicit helper not found")
	}
}
