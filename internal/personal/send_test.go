package personal

import (
	"context"
	"errors"
	"testing"

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
