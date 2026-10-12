package state

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func receiveApply(t *testing.T, messages <-chan tea.Msg) ApplyMsg {
	t.Helper()
	select {
	case msg := <-messages:
		apply, ok := msg.(ApplyMsg)
		if !ok {
			t.Fatalf("got %T, want ApplyMsg", msg)
		}
		return apply
	case <-time.After(time.Second):
		t.Fatal("Post did not deliver")
	}
	return ApplyMsg{}
}

func TestPostQueuesStartupAndNeverMutatesOnSender(t *testing.T) {
	SetSender(nil)
	t.Cleanup(ClosePost)
	applied := []int{}
	Post(func() { applied = append(applied, 1) })
	Post(func() { applied = append(applied, 2) })
	if len(applied) != 0 {
		t.Fatal("startup work executed without Update")
	}
	messages := make(chan tea.Msg, 8)
	SetSender(func(msg tea.Msg) { messages <- msg })
	receiveApply(t, messages).Fn()
	receiveApply(t, messages).Fn()
	if len(applied) != 2 || applied[0] != 1 || applied[1] != 2 {
		t.Fatalf("order: %v", applied)
	}
	ClosePost()
	Post(func() { t.Error("late work executed") })
}

func TestPostReturnsBeforeProgramStarts(t *testing.T) {
	SetSender(nil)
	t.Cleanup(ClosePost)
	Post(func() {})
	messages := make(chan tea.Msg)
	// An unbuffered sender models Program.Send before Run.
	SetSender(func(msg tea.Msg) { messages <- msg })
	Post(func() {})
	receiveApply(t, messages)
	receiveApply(t, messages)
}

func TestAsyncTimeoutAppliesSeriallyAndEndsLoading(t *testing.T) {
	S = &AppState{QueryTimeout: time.Millisecond}
	SetSender(nil)
	t.Cleanup(ClosePost)
	messages := make(chan tea.Msg, 8)
	SetSender(func(msg tea.Msg) { messages <- msg })
	applied := false
	Async("Loading test", func(ctx context.Context) func() {
		<-ctx.Done()
		return func() { applied = true }
	})
	_, _, loading := Status()
	if loading == "" {
		t.Fatal("missing loading indicator")
	}
	msg := receiveApply(t, messages)
	if applied {
		t.Fatal("worker mutated UI state")
	}
	msg.Fn()
	flash, isError, loading := Status()
	if !applied || !isError || flash == "" || loading != "" {
		t.Fatalf("status: %q %v %q", flash, isError, loading)
	}
}
