package sender

import "sync/atomic"

func nextShardIndex(next *atomic.Uint64, concurrency int) int {
	if concurrency <= 1 {
		return 0
	}
	if concurrency&(concurrency-1) == 0 {
		return int(next.Add(1)-1) & (concurrency - 1)
	}
	bound := uint64(concurrency)
	for {
		cur := next.Load()
		if cur >= bound {
			if next.CompareAndSwap(cur, 0) {
				return 0
			}
			continue
		}
		nxt := cur + 1
		if nxt == bound {
			nxt = 0
		}
		if next.CompareAndSwap(cur, nxt) {
			return int(cur)
		}
	}
}
