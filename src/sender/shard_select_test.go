package sender

import (
	"sync/atomic"
	"testing"
)

func TestNextShardIndexNonPowerOfTwoRoundRobin(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
		nextShardIndex(&next, 3),
	}
	want := []int{0, 1, 2, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

func TestNextShardIndexPowerOfTwoUsesMaskPath(t *testing.T) {
	var next atomic.Uint64
	got := []int{
		nextShardIndex(&next, 4),
		nextShardIndex(&next, 4),
		nextShardIndex(&next, 4),
		nextShardIndex(&next, 4),
		nextShardIndex(&next, 4),
	}
	want := []int{0, 1, 2, 3, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

func TestNextShardIndexRecoversWhenCounterOutOfBound(t *testing.T) {
	var next atomic.Uint64
	next.Store(100)
	if got := nextShardIndex(&next, 3); got != 0 {
		t.Fatalf("idx=%d want=0", got)
	}
	if got := nextShardIndex(&next, 3); got != 0 {
		t.Fatalf("idx=%d want=0", got)
	}
	if got := nextShardIndex(&next, 3); got != 1 {
		t.Fatalf("idx=%d want=1", got)
	}
}
func TestKafkaPickShardRoundRobin(t *testing.T) {
	s := &KafkaSender{concurrency: 3}
	got := []int{s.pickShard(), s.pickShard(), s.pickShard(), s.pickShard(), s.pickShard()}
	want := []int{0, 1, 2, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx[%d]=%d want=%d", i, got[i], want[i])
		}
	}
}

func TestUDPPickShardRoundRobin(t *testing.T) {
	u := &UDPUnicastSender{concurrency: 2}
	m := &UDPMulticastSender{concurrency: 2}
	if u.pickShard() != 0 || u.pickShard() != 1 || u.pickShard() != 0 {
		t.Fatalf("udp unicast round robin failed")
	}
	if m.pickShard() != 0 || m.pickShard() != 1 || m.pickShard() != 0 {
		t.Fatalf("udp multicast round robin failed")
	}
}

func TestSFTPPickShardTransferAffinityWithRoundRobinAssignment(t *testing.T) {
	s := &SFTPSender{concurrency: 3, transferShard: map[string]int{}}
	if got := s.pickShard("t1"); got != 0 {
		t.Fatalf("t1 first shard=%d want=0", got)
	}
	if got := s.pickShard("t2"); got != 1 {
		t.Fatalf("t2 first shard=%d want=1", got)
	}
	if got := s.pickShard("t1"); got != 0 {
		t.Fatalf("t1 should keep affinity, got=%d", got)
	}
	if got := s.pickShard("t3"); got != 2 {
		t.Fatalf("t3 first shard=%d want=2", got)
	}
}
