package sender

import "sync/atomic"

func nextShardIndex(next *atomic.Uint64, shardMask int) int {
	if shardMask <= 0 {
		return 0
	}
	return int(next.Add(1)) & shardMask
}
