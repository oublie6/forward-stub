package selector

import "testing"

func BenchmarkCompiledMatchHit(b *testing.B) {
	s := NewCompiled[int]("bench", 1)
	s.ValuesByKey["proto|remote"] = []int{1, 2, 3}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := s.Match("proto|remote"); len(got) != 3 {
			b.Fatalf("match miss")
		}
	}
}

func BenchmarkCompiledMatchDefault(b *testing.B) {
	s := NewDefaultOnlyCompiled[int]("bench", []int{1, 2, 3})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := s.Match("missing"); len(got) != 3 {
			b.Fatalf("default miss")
		}
	}
}
