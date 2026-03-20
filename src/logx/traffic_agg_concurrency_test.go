package logx

import (
	"sync"
	"testing"
)

// TestTrafficCounterCloseConcurrentWithAddBytes 验证聚合统计句柄在“热路径写入”和“生命周期释放”
// 并发交错时不会发生 panic、重复释放或显式数据竞争。
//
// 覆盖点：
// 1. AddBytes 与 Close 并发；
// 2. Close 多次调用幂等；
// 3. receiver 风格的 fields 维度可正常创建共享 counter。
func TestTrafficCounterCloseConcurrentWithAddBytes(t *testing.T) {
	// 使用 receiver 风格标签，模拟真实接收端在高并发收包时的统计对象。
	tc := AcquireTrafficCounter("test traffic", "role", "receiver", "receiver", "r1", "receiver_key", "k1", "proto", "udp")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		// 连续高频写入，模拟热路径原子计数。
		for i := 0; i < 20000; i++ {
			tc.AddBytes(256)
		}
	}()

	go func() {
		defer wg.Done()
		// 重复关闭，模拟热重载或异常恢复路径中的多次释放尝试。
		for i := 0; i < 1000; i++ {
			tc.Close()
		}
	}()

	wg.Wait()
	// 再次关闭应保持幂等。
	tc.Close()
}
