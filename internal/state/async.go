package state

import (
	"context"
	"time"
)

// Async runs work off the UI goroutine with a QueryTimeout-bounded context.
// The closure it returns (if any) is then run on the UI goroutine. When
// loading is non-empty a loading indicator is shown for the duration.
func Async(loading string, work func(ctx context.Context) func()) {
	AsyncTimeout(loading, S.QueryTimeout, work)
}

// AsyncTimeout is Async with an explicit timeout; 0 means no timeout.
func AsyncTimeout(loading string, timeout time.Duration, work func(ctx context.Context) func()) {
	tok := -1
	if loading != "" {
		tok = BeginLoading(loading)
	}
	go func() {
		var ctx context.Context
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		apply := work(ctx)
		cancel()
		S.App.QueueUpdateDraw(func() {
			if tok >= 0 {
				EndLoading(tok)
			}
			if apply != nil {
				apply()
			}
		})
	}()
}
