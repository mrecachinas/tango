package state

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

type ApplyMsg struct{ Fn func() }

var dispatcher struct {
	sync.Mutex
	pending    []tea.Msg
	wake, stop chan struct{}
	closed     bool
}

// SetSender binds Post to Program.Send or an injectable message queue.
// Delivery runs off the UI goroutine, so startup work and nested Post calls
// cannot deadlock on Program.Send before Run or from inside Update.
// Pass nil before constructing a new application to queue startup work.
func SetSender(send func(tea.Msg)) {
	dispatcher.Lock()
	if dispatcher.stop != nil {
		close(dispatcher.stop)
	}
	dispatcher.closed = false
	dispatcher.stop, dispatcher.wake = nil, nil
	if send != nil {
		wake, stop := make(chan struct{}, 1), make(chan struct{})
		dispatcher.wake, dispatcher.stop = wake, stop
		wake <- struct{}{}
		go deliver(send, wake, stop)
	}
	dispatcher.Unlock()
}

func deliver(send func(tea.Msg), wake, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		dispatcher.Lock()
		if dispatcher.stop != stop {
			dispatcher.Unlock()
			return
		}
		if len(dispatcher.pending) == 0 {
			dispatcher.Unlock()
			select {
			case <-wake:
				continue
			case <-stop:
				return
			}
		}
		msg := dispatcher.pending[0]
		dispatcher.pending[0] = nil
		dispatcher.pending = dispatcher.pending[1:]
		dispatcher.Unlock()
		send(msg)
	}
}

func ClosePost() {
	dispatcher.Lock()
	defer dispatcher.Unlock()
	if dispatcher.stop != nil {
		close(dispatcher.stop)
	}
	dispatcher.wake, dispatcher.stop = nil, nil
	dispatcher.pending = nil
	dispatcher.closed = true
}

// Shutdown is called on the UI goroutine after Run, before disconnecting.
func Shutdown() {
	if S.LiveStopCh != nil {
		close(S.LiveStopCh)
		S.LiveStopCh = nil
	}
	if S.StatsStopCh != nil {
		close(S.StatsStopCh)
		S.StatsStopCh = nil
	}
	S.LiveMode = false
	S.StatsEpoch++
	ClosePost()
}

// Post queues fn in FIFO order and never executes it itself.
func Post(fn func()) {
	if fn == nil {
		return
	}
	dispatcher.Lock()
	defer dispatcher.Unlock()
	if dispatcher.closed {
		return
	}
	dispatcher.pending = append(dispatcher.pending, ApplyMsg{Fn: fn})
	if dispatcher.wake != nil {
		select {
		case dispatcher.wake <- struct{}{}:
		default:
		}
	}
}
