package engine

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type startupState struct {
	done chan struct{}
	once sync.Once
	err  error
}

func newStartupState() *startupState { return &startupState{done: make(chan struct{})} }
func (s *startupState) finish(err error) {
	s.once.Do(func() { s.err = err; close(s.done) })
}
func (s *startupState) wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

var startup = newStartupState()

// WaitForReady prevents account writes and VPN commands reaching a partially
// started engine. Android keeps the UI usable while its native modules start.
func WaitForReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := startup.wait(ctx); err != nil {
		return fmt.Errorf("Portmaster engine is not ready: %w", err)
	}
	return nil
}
