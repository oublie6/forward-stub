package logx

import "testing"

func BenchmarkTrafficCounterAddBytes(b *testing.B) {
	tc := &TrafficCounter{c: &trafficCounter{sample: 1}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.AddBytes(128)
	}
}

func BenchmarkTrafficCounterAddBytesSampled(b *testing.B) {
	tc := &TrafficCounter{c: &trafficCounter{sample: 16}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tc.AddBytes(128)
	}
}

func BenchmarkTrafficCounterAddBytesParallel(b *testing.B) {
	tc := &TrafficCounter{c: &trafficCounter{sample: 1}}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tc.AddBytes(128)
		}
	})
}
