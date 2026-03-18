package sender

import "sync/atomic"

// nextShardIndex is a package-local helper used by shard_index.go.
func nextShardIndex(next *atomic.Uint64, shardMask int) int {
	if shardMask <= 0 {
		return 0
	}
	return int(next.Add(1)) & shardMask
}
