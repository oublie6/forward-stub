package logx

import (
	"sync"
	"testing"
)

// TestTrafficCounterCloseConcurrentWithAddBytes verifies the TrafficCounterCloseConcurrentWithAddBytes behavior for the logx package.
func TestTrafficCounterCloseConcurrentWithAddBytes(t *testing.T) {
	tc := AcquireTrafficCounter("test traffic", "role", "receiver", "receiver", "r1", "receiver_key", "k1", "proto", "udp")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 20000; i++ {
			tc.AddBytes(256)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			tc.Close()
		}
	}()

	wg.Wait()
	// 再次关闭应保持幂等。
	tc.Close()
}
