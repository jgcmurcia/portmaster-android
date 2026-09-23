package engine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartupWaitsForCompletion(t *testing.T) {
	s := newStartupState()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup must wait: %v", err)
	}
	s.finish(nil)
	if err := s.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestStartupFailureReachesCallers(t *testing.T) {
	s := newStartupState()
	failure := errors.New("database directory unavailable")
	s.finish(failure)
	if err := s.wait(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("lost startup failure: %v", err)
	}
}
