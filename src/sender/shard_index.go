package sender

import "sync/atomic"

// nextShardIndex 是供 shard_index.go 使用的包内辅助函数。
func nextShardIndex(next *atomic.Uint64, shardMask int) int {
	if shardMask <= 0 {
		return 0
	}
	return int(next.Add(1)) & shardMask
}
