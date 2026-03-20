package task

import (
	"testing"

	"forward-stub/src/logx"
)

// TestTaskDetachAndReuseTrafficCounter 验证 task 在热重载式重建时，可以把旧实例的
// 发送聚合统计句柄安全转移到新实例，避免 task 统计在 replace 过程中被清零。
func TestTaskDetachAndReuseTrafficCounter(t *testing.T) {
	// 先构造一个符合真实 task 维度的发送统计句柄。
	counter := logx.AcquireTrafficCounter("task send traffic stats", "role", "task", "task", "demo", "direction", "send")
	t1 := &Task{Name: "demo"}
	t1.ReuseTrafficCounter(counter)
	// 旧 task 停止前分离句柄。
	got := t1.DetachTrafficCounter()
	if got == nil {
		t.Fatalf("expected detached counter")
	}
	t2 := &Task{Name: "demo"}
	// 新 task 接管旧句柄，模拟 runtime.removeTask + addTask 的复用链路。
	t2.ReuseTrafficCounter(got)
	if t2.sendStats == nil {
		t.Fatalf("expected reused counter on new task")
	}
	// 清理时再次分离，最后由原始句柄关闭，验证 Close 可由生命周期管理方统一执行。
	_ = t2.DetachTrafficCounter()
	counter.Close()
}
