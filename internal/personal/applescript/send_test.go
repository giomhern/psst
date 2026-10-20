package applescript

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

func TestSendOutcomesAreSanitizedAndNeverRetried(t *testing.T) {
	for _, tc := range []struct {
		name, reply        string
		started            bool
		failure, errorWant error
		status             domain.SendStatus
	}{
		{name: "accepted", reply: "requested\n", started: true, status: domain.SendRequested},
		{name: "permission", reply: "permission_denied", started: true, errorWant: domain.ErrAutomationDenied},
		{name: "target unavailable", reply: "not_requested", started: true, errorWant: domain.ErrSendUnavailable},
		{name: "start failure", failure: errors.New("PRIVATE body and recipient"), errorWant: domain.ErrSendUnavailable},
		{name: "timeout after launch", started: true, failure: context.DeadlineExceeded, errorWant: domain.ErrSendUnknown, status: domain.SendUnknown},
		{name: "unknown reply", started: true, reply: "PRIVATE", errorWant: domain.ErrSendUnknown, status: domain.SendUnknown},
		{name: "oversized reply", started: true, reply: strings.Repeat("x", 65), errorWant: domain.ErrSendUnknown, status: domain.SendUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			text := "Quotes \" and \\ plus $(not code)\n🙂"
			result, err := request(context.Background(), "iMessage;-;synthetic", text, func(ctx context.Context, guid, body string) ([]byte, bool, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 30*time.Second || guid != "iMessage;-;synthetic" || body != text {
					t.Fatal("unsafe or unbounded arguments")
				}
				return []byte(tc.reply), tc.started, tc.failure
			})
			if calls != 1 || result.Status != tc.status || !errors.Is(err, tc.errorWant) {
				t.Fatalf("result %v; error %v", result, err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw automation failure escaped")
			}
		})
	}
}

func TestInvalidSendNeverStartsAutomation(t *testing.T) {
	for _, tc := range []struct{ guid, text string }{
		{"", "hello"}, {"-e", "hello"}, {"valid", ""}, {"valid", " \n"},
		{"valid", "null\x00"}, {"valid", string([]byte{0xff})},
		{strings.Repeat("x", 1025), "hello"}, {"valid", strings.Repeat("猫", 4001)},
	} {
		_, err := request(context.Background(), tc.guid, tc.text, func(context.Context, string, string) ([]byte, bool, error) {
			t.Fatal("invalid input started a process")
			return nil, false, nil
		})
		if err == nil {
			t.Fatal("invalid send was accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := request(ctx, "synthetic", "hello", func(context.Context, string, string) ([]byte, bool, error) {
		t.Fatal("canceled send started a process")
		return nil, false, nil
	})
	if !errors.Is(err, domain.ErrSendUnavailable) {
		t.Fatal("canceled send was not rejected")
	}
}
