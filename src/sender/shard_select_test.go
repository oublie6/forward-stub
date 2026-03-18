package sender

import (
	"sync/atomic"
	"testing"
)

// TestNextShardIndexPowerOfTwoRoundRobin verifies the NextShardIndexPowerOfTwoRoundRobin behavior for the sender package.
func TestNextShardIndexPowerOfTwoRoundRobin(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
	}
	want := []int{1, 2, 3, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestNextShardIndexSingleShardAlwaysZero verifies the NextShardIndexSingleShardAlwaysZero behavior for the sender package.
func TestNextShardIndexSingleShardAlwaysZero(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 0),
		nextShardIndex(&next, 0),
		nextShardIndex(&next, 0),
	}
	want := []int{0, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestNextShardIndexUsesBitMaskWhenCounterOutOfBound verifies the NextShardIndexUsesBitMaskWhenCounterOutOfBound behavior for the sender package.
func TestNextShardIndexUsesBitMaskWhenCounterOutOfBound(t *testing.T) {
	var next atomic.Uint64
	next.Store(100)
	if got := nextShardIndex(&next, 3); got != 1 {
		t.Fatalf("idx=%d want=1", got)
	}
	if got := nextShardIndex(&next, 3); got != 2 {
		t.Fatalf("idx=%d want=2", got)
	}
	if got := nextShardIndex(&next, 3); got != 3 {
		t.Fatalf("idx=%d want=3", got)
	}
}

// TestKafkaNextShardIndexRoundRobin verifies the KafkaNextShardIndexRoundRobin behavior for the sender package.
func TestKafkaNextShardIndexRoundRobin(t *testing.T) {
	s := &KafkaSender{concurrency: 4, shardMask: 3}
	got := []int{
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
		nextShardIndex(&s.nextIdx, s.shardMask),
	}
	want := []int{1, 2, 3, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

// TestUDPPickShardRoundRobin verifies the UDPPickShardRoundRobin behavior for the sender package.
func TestUDPPickShardRoundRobin(t *testing.T) {
	u := &UDPUnicastSender{concurrency: 2, shardMask: 1}
	m := &UDPMulticastSender{concurrency: 2, shardMask: 1}
	if nextShardIndex(&u.nextIdx, u.shardMask) != 1 || nextShardIndex(&u.nextIdx, u.shardMask) != 0 || nextShardIndex(&u.nextIdx, u.shardMask) != 1 {
		t.Fatalf("udp unicast round robin failed")
	}
	if nextShardIndex(&m.nextIdx, m.shardMask) != 1 || nextShardIndex(&m.nextIdx, m.shardMask) != 0 || nextShardIndex(&m.nextIdx, m.shardMask) != 1 {
		t.Fatalf("udp multicast round robin failed")
	}
}

// TestSFTPPickShardTransferAffinityWithRoundRobinAssignment verifies the SFTPPickShardTransferAffinityWithRoundRobinAssignment behavior for the sender package.
func TestSFTPPickShardTransferAffinityWithRoundRobinAssignment(t *testing.T) {
	s := &SFTPSender{concurrency: 4, shardMask: 3, transferShard: map[string]int{}}
	if got := s.pickShard("t1"); got != 1 {
		t.Fatalf("t1 first shard=%d want=1", got)
	}
	if got := s.pickShard("t2"); got != 2 {
		t.Fatalf("t2 first shard=%d want=2", got)
	}
	if got := s.pickShard("t1"); got != 1 {
		t.Fatalf("t1 should keep affinity, got=%d", got)
	}
	if got := s.pickShard("t3"); got != 3 {
		t.Fatalf("t3 first shard=%d want=3", got)
	}
}
