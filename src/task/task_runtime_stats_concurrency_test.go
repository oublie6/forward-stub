package task

import (
	"sync"
	"testing"
)

// TestRuntimeStatsConcurrentWithStopGraceful 验证聚合统计线程读取 task 运行时快照时，
// 即使 task 正在 StopGraceful，也不会因为 pool/channel 资源切换而产生崩溃。
func TestRuntimeStatsConcurrentWithStopGraceful(t *testing.T) {
	// 选择 channel 模型，覆盖 StopGraceful 中 close(ch)+wg.Wait 的并发窗口。
	tk := &Task{
		Name:           "task-concurrency",
		ExecutionModel: ExecutionModelChannel,
		QueueSize:      64,
	}
	if err := tk.Start(); err != nil {
		t.Fatalf("start task: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// 模拟聚合日志线程高频读取 runtimeStats。
		for i := 0; i < 5000; i++ {
			_ = tk.runtimeStats()
		}
	}()

	// 与 runtimeStats 并发执行优雅停止，验证状态锁设计足够稳健。
	tk.StopGraceful()
	wg.Wait()
}
