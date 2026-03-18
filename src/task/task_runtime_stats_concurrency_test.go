package task

import (
	"sync"
	"testing"
)

// TestRuntimeStatsConcurrentWithStopGraceful verifies the RuntimeStatsConcurrentWithStopGraceful behavior for the task package.
func TestRuntimeStatsConcurrentWithStopGraceful(t *testing.T) {
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
		for i := 0; i < 5000; i++ {
			_ = tk.runtimeStats()
		}
	}()

	tk.StopGraceful()
	wg.Wait()
}
