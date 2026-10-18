package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContactCommandsAreExplicitAndDoNotOpenMessages(t *testing.T) {
	for _, tc := range []struct {
		action, status       string
		authorize, wantError bool
	}{
		{"status", "permission_required", false, false},
		{"authorize", "authorized", true, false},
		{"authorize", "limited", true, false},
		{"authorize", "denied", true, true},
		{"status", "helper_missing", false, false},
	} {
		var out bytes.Buffer
		err := execute(context.Background(), []string{"personal", "contacts", tc.action}, &out, dependencies{
			openReader: func(context.Context) (conversationReader, error) {
				t.Fatal("permission command opened Messages")
				return nil, nil
			},
			contactsAccess: func(ctx context.Context, authorize bool) (string, error) {
				if authorize != tc.authorize {
					t.Fatal("incorrect permission action")
				}
				bound := 3 * time.Second
				if authorize {
					bound = 90 * time.Second
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > bound {
					t.Fatal("unbounded permission operation")
				}
				return tc.status, nil
			},
		})
		if (err != nil) != tc.wantError || !strings.Contains(out.String(), tc.status) {
			t.Fatalf("output = %q, error = %v", out.String(), err)
		}
	}
}

func TestContactHelpAndInvalidArgumentsNeverRequestAccess(t *testing.T) {
	for _, args := range [][]string{
		{"personal", "contacts", "--help"}, {"personal", "contacts", "authorize", "--help"},
		{"personal", "contacts", "authorize", "extra"}, {"personal", "contacts", "status", "--unknown"},
	} {
		var out bytes.Buffer
		_ = execute(context.Background(), args, &out, dependencies{contactsAccess: func(context.Context, bool) (string, error) { t.Fatal("unexpected contact access"); return "", nil }})
	}
}

func TestContactFailuresAndCancellationAreSafe(t *testing.T) {
	var out bytes.Buffer
	err := execute(context.Background(), []string{"personal", "contacts", "status"}, &out, dependencies{contactsAccess: func(context.Context, bool) (string, error) { return "", errors.New("private helper details") }})
	if err == nil || strings.Contains(err.Error(), "private") || out.Len() != 0 {
		t.Fatalf("unsafe failure: %v, %s", err, out.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = execute(ctx, []string{"personal", "contacts", "authorize"}, &out, dependencies{contactsAccess: func(ctx context.Context, _ bool) (string, error) { return "", ctx.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
