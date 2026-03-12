package sender

import "sync/atomic"

func nextShardIndex(next *atomic.Uint64, concurrency int) int {
	if concurrency <= 1 {
		return 0
	}
	return int(next.Add(1)-1) & (concurrency - 1)
}
