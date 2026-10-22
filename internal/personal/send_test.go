package personal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

type fakeSendTarget struct {
	closed              bool
	lookupErr, closeErr error
}

func (r *fakeSendTarget) sendTarget(_ context.Context, id string) (string, error) {
	if id != "7" {
		return "", errors.New("wrong ID")
	}
	return "iMessage;-;synthetic", r.lookupErr
}
func (r *fakeSendTarget) Close() error { r.closed = true; return r.closeErr }

func TestSendClosesTargetBeforeRequest(t *testing.T) {
	reader := &fakeSendTarget{}
	calls := 0
	result, err := send(context.Background(), "7", "Synthetic text", func(context.Context) (sendTargetReader, error) { return reader, nil }, func(_ context.Context, guid, text string) (domain.SendResult, error) {
		calls++
		if !reader.closed || guid != "iMessage;-;synthetic" || text != "Synthetic text" {
			t.Fatal("send destination or resource ownership wrong")
		}
		return domain.SendResult{Status: domain.SendRequested}, nil
	})
	if err != nil || result.Status != domain.SendRequested || calls != 1 {
		t.Fatalf("result %v; error %v", result, err)
	}
}

func TestTargetFailurePreventsSend(t *testing.T) {
	for _, reader := range []*fakeSendTarget{{lookupErr: errors.New("PRIVATE lookup")}, {closeErr: errors.New("PRIVATE close")}} {
		_, err := send(context.Background(), "7", "Synthetic", func(context.Context) (sendTargetReader, error) { return reader, nil }, func(context.Context, string, string) (domain.SendResult, error) {
			t.Fatal("target failure started automation")
			return domain.SendResult{}, nil
		})
		if !reader.closed || !errors.Is(err, domain.ErrSendUnavailable) {
			t.Fatal("failed lookup leaked resources or raw errors")
		}
	}
	for _, id := range []string{"invalid", "0"} {
		_, err := send(context.Background(), id, "Synthetic", func(context.Context) (sendTargetReader, error) {
			t.Fatal("invalid ID opened the database")
			return nil, nil
		}, nil)
		if err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}

type fakeObserver struct {
	fakeSendTarget
	checkpointErr error
	receipts      []domain.SendReceipt
	matchErr      error
	checks        int
}

func (r *fakeObserver) sendCheckpoint(context.Context) (int64, error) { return 10, r.checkpointErr }
func (r *fakeObserver) matchSend(_ context.Context, id, guid, text string, after int64, started, ended time.Time) (domain.SendReceipt, error) {
	if id != "7" || guid != "synthetic-chat" || text != "Synthetic" || after != 10 || ended.Before(started) {
		return domain.SendReceipt{}, domain.ErrReceiptUnavailable
	}
	i := min(r.checks, len(r.receipts)-1)
	r.checks++
	if i < 0 {
		return domain.SendReceipt{}, r.matchErr
	}
	return r.receipts[i], r.matchErr
}

func TestObservationWindowAndCompetingMatches(t *testing.T) {
	for _, tc := range []struct {
		name     string
		receipts []domain.SendReceipt
		err      error
		want     bool
	}{
		{name: "delayed unique", receipts: []domain.SendReceipt{{}, {GUID: "synthetic-message", Status: domain.DeliverySent}}, want: true},
		{name: "none"},
		{name: "ambiguous", err: errors.New("PRIVATE ambiguous database detail")},
		{name: "competing GUIDs", receipts: []domain.SendReceipt{{GUID: "first"}, {GUID: "second"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeObserver{receipts: tc.receipts, matchErr: tc.err}
			waits := 0
			got := observeRequested(context.Background(), "7", "synthetic-chat", "Synthetic", 10, time.Now(), func(context.Context) (sendTargetReader, error) { return reader, nil }, func(context.Context) error { waits++; return nil })
			if !reader.closed || (got.GUID != "") != tc.want || waits > 6 {
				t.Fatal("observation failed resource, ambiguity, or work bound")
			}
			if tc.want && waits != 6 {
				t.Fatal("first arrival bypassed ambiguity window")
			}
		})
	}
}

func TestObservationCancellationClosesReader(t *testing.T) {
	reader := &fakeObserver{}
	got := observeRequested(context.Background(), "7", "synthetic-chat", "Synthetic", 10, time.Now(), func(context.Context) (sendTargetReader, error) { return reader, nil }, func(context.Context) error { return context.Canceled })
	if !reader.closed || reader.checks != 0 || got.GUID != "" {
		t.Fatal("canceled observation did work or retained receipt")
	}
}

func TestCheckpointFailureDoesNotResendOrChangeAcceptance(t *testing.T) {
	reader := &fakeObserver{checkpointErr: errors.New("PRIVATE checkpoint failure")}
	calls := 0
	got, err := send(context.Background(), "7", "Synthetic", func(context.Context) (sendTargetReader, error) { return reader, nil }, func(context.Context, string, string) (domain.SendResult, error) {
		calls++
		return domain.SendResult{Status: domain.SendRequested}, nil
	})
	if err != nil || got.Status != domain.SendRequested || got.Receipt.GUID != "" || calls != 1 {
		t.Fatal("tracking failure changed send acceptance")
	}
}
