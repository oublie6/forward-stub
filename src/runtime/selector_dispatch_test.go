package runtime

import (
	"context"
	"reflect"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// TestMatchDispatchTasksSelectorPriority verifies the MatchDispatchTasksSelectorPriority behavior for the runtime package.
func TestMatchDispatchTasksSelectorPriority(t *testing.T) {
	st := NewStore()
	st.tasks = map[string]*TaskState{
		"default":  {Name: "default"},
		"exact":    {Name: "exact"},
		"cidr":     {Name: "cidr"},
		"port":     {Name: "port"},
		"range":    {Name: "range"},
		"combined": {Name: "combined"},
	}
	st.selectorCfg = map[string]config.SelectorConfig{
		"default": {Receivers: []string{"r1"}, Tasks: []string{"default"}},
		"exact": {
			Receivers: []string{"r1"},
			Tasks:     []string{"exact"},
			Source: &config.SourceSelectorConfig{
				SrcCIDRs:      []string{"10.0.0.1"},
				SrcPortRanges: []string{"9000"},
			},
		},
		"cidr": {
			Receivers: []string{"r1"},
			Tasks:     []string{"cidr"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.0/24"}},
		},
		"port": {
			Receivers: []string{"r1"},
			Tasks:     []string{"port"},
			Source:    &config.SourceSelectorConfig{SrcPortRanges: []string{"7000"}},
		},
		"range": {
			Receivers: []string{"r1"},
			Tasks:     []string{"range"},
			Source:    &config.SourceSelectorConfig{SrcPortRanges: []string{"8000-8005"}},
		},
		"combined": {
			Receivers: []string{"r1"},
			Tasks:     []string{"combined"},
			Source: &config.SourceSelectorConfig{
				SrcCIDRs:      []string{"10.0.1.0/24"},
				SrcPortRanges: []string{"8500-8510"},
			},
		},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}

	cases := []struct {
		name string
		pkt  *packet.Packet
		want []string
	}{
		{name: "default on invalid remote", pkt: &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: "not-an-addr"}}}, want: []string{"default"}},
		{name: "default when all source selectors miss", pkt: testSelectorPacket("192.0.2.10:9999"), want: []string{"default"}},
		{name: "exact ip port", pkt: testSelectorPacket("10.0.0.1:9000"), want: []string{"exact", "cidr"}},
		{name: "exact ip", pkt: testSelectorPacket("10.0.0.2:6553"), want: []string{"cidr"}},
		{name: "exact port", pkt: testSelectorPacket("10.9.9.9:7000"), want: []string{"port"}},
		{name: "port range", pkt: testSelectorPacket("10.9.9.9:8003"), want: []string{"range"}},
		{name: "cidr and port range", pkt: testSelectorPacket("10.0.1.3:8505"), want: []string{"combined"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskNames(st.matchDispatchTasks("r1", tc.pkt))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("matchDispatchTasks=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestMatchDispatchTasksUsesStructuredSourceMetadata verifies the MatchDispatchTasksUsesStructuredSourceMetadata behavior for the runtime package.
func TestMatchDispatchTasksUsesStructuredSourceMetadata(t *testing.T) {
	st := NewStore()
	st.tasks = map[string]*TaskState{"exact": {Name: "exact"}}
	st.selectorCfg = map[string]config.SelectorConfig{
		"exact": {
			Receivers: []string{"r1"},
			Tasks:     []string{"exact"},
			Source: &config.SourceSelectorConfig{
				SrcCIDRs:      []string{"10.0.0.1"},
				SrcPortRanges: []string{"9000"},
			},
		},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}

	pkt := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: "ignored", SrcIPv4: 0x0a000001, SrcPort: 9000, HasSrcAddr: true}}}
	got := st.matchDispatchTasks("r1", pkt)
	if want := []string{"exact"}; !reflect.DeepEqual(taskNames(got), want) {
		t.Fatalf("expected structured source hit %v, got=%v", want, taskNames(got))
	}
}

// TestMatchDispatchTasksDedupesTaskAcrossBucketsAndSelectors verifies the MatchDispatchTasksDedupesTaskAcrossBucketsAndSelectors behavior for the runtime package.
func TestMatchDispatchTasksDedupesTaskAcrossBucketsAndSelectors(t *testing.T) {
	st := NewStore()
	st.tasks = map[string]*TaskState{
		"shared": {Name: "shared"},
		"extra":  {Name: "extra"},
	}
	st.selectorCfg = map[string]config.SelectorConfig{
		"by-ip": {
			Receivers: []string{"r1"},
			Tasks:     []string{"shared"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.1"}},
		},
		"cidr-range": {
			Receivers: []string{"r1"},
			Tasks:     []string{"shared", "extra"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.0/24"}, SrcPortRanges: []string{"9000-9010"}},
		},
		"port": {
			Receivers: []string{"r1"},
			Tasks:     []string{"shared"},
			Source:    &config.SourceSelectorConfig{SrcPortRanges: []string{"9001"}},
		},
		"default": {Receivers: []string{"r1"}, Tasks: []string{"shared"}},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}
	got := st.matchDispatchTasks("r1", testSelectorPacket("10.0.0.1:9001"))
	if want := []string{"shared", "extra"}; !reflect.DeepEqual(taskNames(got), want) {
		t.Fatalf("expected deduped source matches %v, got=%v", want, taskNames(got))
	}
}

// TestMatchDispatchTasksReturnsEmptyWhenNoDefaultAndNoSourceMatch verifies the MatchDispatchTasksReturnsEmptyWhenNoDefaultAndNoSourceMatch behavior for the runtime package.
func TestMatchDispatchTasksReturnsEmptyWhenNoDefaultAndNoSourceMatch(t *testing.T) {
	st := NewStore()
	st.tasks = map[string]*TaskState{"t1": {Name: "t1"}}
	st.selectorCfg = map[string]config.SelectorConfig{
		"src": {
			Receivers: []string{"r1"},
			Tasks:     []string{"t1"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.1"}},
		},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}
	got := st.matchDispatchTasks("r1", testSelectorPacket("192.0.2.3:9000"))
	if len(got) != 0 {
		t.Fatalf("expected no matched tasks, got=%v", taskNames(got))
	}
}

// TestDispatchUsesSelectorSnapshotForCloneFanout verifies the DispatchUsesSelectorSnapshotForCloneFanout behavior for the runtime package.
func TestDispatchUsesSelectorSnapshotForCloneFanout(t *testing.T) {
	ctx := context.Background()
	s1 := &captureSender{name: "s1"}
	s2 := &captureSender{name: "s2"}
	t1 := &task.Task{Name: "t1", FastPath: true, Senders: []sender.Sender{s1}}
	t2 := &task.Task{Name: "t2", FastPath: true, Senders: []sender.Sender{s2}}
	if err := t1.Start(); err != nil {
		t.Fatalf("t1 start: %v", err)
	}
	defer t1.StopGraceful()
	if err := t2.Start(); err != nil {
		t.Fatalf("t2 start: %v", err)
	}
	defer t2.StopGraceful()

	st := NewStore()
	st.tasks = map[string]*TaskState{
		"t1": {Name: "t1", T: t1},
		"t2": {Name: "t2", T: t2},
	}
	st.selectorCfg = map[string]config.SelectorConfig{
		"sel": {
			Receivers: []string{"r1"},
			Tasks:     []string{"t1", "t2"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"192.0.2.1"}},
		},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}

	payload := []byte("selector-fanout")
	pkt := testSelectorPacket("192.0.2.1:5000")
	pkt.Payload = append([]byte(nil), payload...)
	released := 0
	pkt.ReleaseFn = func() { released++ }

	dispatch(ctx, st, "r1", pkt)

	if got := string(s1.Last()); got != string(payload) {
		t.Fatalf("task1 payload mismatch: got=%q want=%q", got, payload)
	}
	if got := string(s2.Last()); got != string(payload) {
		t.Fatalf("task2 payload mismatch: got=%q want=%q", got, payload)
	}
	if released != 1 {
		t.Fatalf("expected original packet released once, got=%d", released)
	}
}

// BenchmarkMatchDispatchTasksExactIPv4 benchmarks the MatchDispatchTasksExactIPv4 behavior for the runtime package.
func BenchmarkMatchDispatchTasksExactIPv4(b *testing.B) {
	buildStore := func() *Store {
		st := NewStore()
		st.tasks = map[string]*TaskState{"exact": {Name: "exact"}}
		st.selectorCfg = map[string]config.SelectorConfig{
			"exact": {
				Receivers: []string{"r1"},
				Tasks:     []string{"exact"},
				Source: &config.SourceSelectorConfig{
					SrcCIDRs:      []string{"10.0.0.1"},
					SrcPortRanges: []string{"9000"},
				},
			},
		}
		if err := st.refreshDispatchSubs(); err != nil {
			b.Fatalf("refresh dispatch: %v", err)
		}
		return st
	}

	b.Run("structured_meta", func(b *testing.B) {
		st := buildStore()
		pkt := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{SrcIPv4: 0x0a000001, SrcPort: 9000, HasSrcAddr: true}}}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if got := st.matchDispatchTasks("r1", pkt); len(got) != 1 {
				b.Fatalf("unexpected tasks: %v", taskNames(got))
			}
		}
	})

	b.Run("remote_parse_fallback", func(b *testing.B) {
		st := buildStore()
		pkt := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: "10.0.0.1:9000"}}}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if got := st.matchDispatchTasks("r1", pkt); len(got) != 1 {
				b.Fatalf("unexpected tasks: %v", taskNames(got))
			}
		}
	})
}

// taskNames is a package-local helper used by selector_dispatch_test.go.
func taskNames(tasks []*TaskState) []string {
	out := make([]string, 0, len(tasks))
	for _, ts := range tasks {
		out = append(out, ts.Name)
	}
	return out
}

func testSelectorPacket(remote string) *packet.Packet {
	pkt := &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: remote}}}
	pkt.Meta.SetSourceFromRemote(remote)
	return pkt
}
