package schedule

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeElector struct {
	leader atomic.Bool
}

func (f *fakeElector) IsLeader(context.Context) error {
	if f.leader.Load() {
		return nil
	}
	return errors.New("not leader")
}

func TestManagerLocalGroupStartsIntervalJob(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	var executed atomic.Int32
	done := make(chan struct{}, 1)

	if err := mgr.Register(RegistrarFunc(func(m *Manager) error {
		group, err := m.NewGroup("local-test", ModeLocal)
		if err != nil {
			return err
		}
		return group.Every("tick", 10*time.Millisecond, func(context.Context) error {
			if executed.Add(1) == 1 {
				select {
				case done <- struct{}{}:
				default:
				}
			}
			return nil
		}, WithImmediateStart())
	})); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("local group job did not run")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := mgr.Stop(stopCtx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestManagerLeaderOnlyGroupDoesNotRunWhenNotLeader(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	elector := &fakeElector{}
	var executed atomic.Int32

	if err := mgr.Register(RegistrarFunc(func(m *Manager) error {
		group, err := m.NewGroup("leader-test", ModeLeaderOnly,
			WithElector(elector),
			WithLeaderCheckInterval(10*time.Millisecond),
		)
		if err != nil {
			return err
		}
		return group.Every("tick", 10*time.Millisecond, func(context.Context) error {
			executed.Add(1)
			return nil
		}, WithImmediateStart())
	})); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if ok := waitForCondition(t, 80*time.Millisecond, func() bool {
		return executed.Load() > 0
	}); ok {
		got := executed.Load()
		t.Fatalf("leader_only group should not run when not leader, got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := mgr.Stop(stopCtx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestManagerLeaderOnlyGroupStartsAfterBecomingLeader(t *testing.T) {
	t.Parallel()

	mgr := NewManager(nil)
	elector := &fakeElector{}
	var executed atomic.Int32
	done := make(chan struct{}, 1)

	if err := mgr.Register(RegistrarFunc(func(m *Manager) error {
		group, err := m.NewGroup("leader-test", ModeLeaderOnly,
			WithElector(elector),
			WithLeaderCheckInterval(10*time.Millisecond),
		)
		if err != nil {
			return err
		}
		return group.Every("tick", 10*time.Millisecond, func(context.Context) error {
			if executed.Add(1) == 1 {
				select {
				case done <- struct{}{}:
				default:
				}
			}
			return nil
		}, WithImmediateStart())
	})); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if ok := waitForCondition(t, 40*time.Millisecond, func() bool {
		return executed.Load() > 0
	}); ok {
		got := executed.Load()
		t.Fatalf("leader_only group should not run before leader election succeeds, got %d", got)
	}

	elector.leader.Store(true)
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("leader_only group did not run after becoming leader")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := mgr.Stop(stopCtx); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}
