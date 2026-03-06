package task

import (
	"testing"

	"forward-stub/src/logx"
)

func TestTaskDetachAndReuseTrafficCounter(t *testing.T) {
	counter := logx.AcquireTrafficCounter("task send traffic stats", "role", "task", "task", "demo", "direction", "send")
	t1 := &Task{Name: "demo"}
	t1.ReuseTrafficCounter(counter)
	got := t1.DetachTrafficCounter()
	if got == nil {
		t.Fatalf("expected detached counter")
	}
	t2 := &Task{Name: "demo"}
	t2.ReuseTrafficCounter(got)
	if t2.sendStats == nil {
		t.Fatalf("expected reused counter on new task")
	}
	_ = t2.DetachTrafficCounter()
	counter.Close()
}
