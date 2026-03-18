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

func TestMatchDispatchTasksSelectorSourceModes(t *testing.T) {
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
		name   string
		remote string
		want   []string
	}{
		{name: "default on invalid remote", remote: "not-an-addr", want: []string{"default"}},
		{name: "exact ip port", remote: "10.0.0.1:9000", want: []string{"exact", "cidr", "default"}},
		{name: "cidr only", remote: "10.0.0.2:6553", want: []string{"cidr", "default"}},
		{name: "single port", remote: "10.9.9.9:7000", want: []string{"port", "default"}},
		{name: "port range", remote: "10.9.9.9:8003", want: []string{"range", "default"}},
		{name: "cidr and port range", remote: "10.0.1.3:8505", want: []string{"combined", "default"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskNames(st.matchDispatchTasks("r1", &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: tc.remote}}}))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("matchDispatchTasks(%q)=%v want=%v", tc.remote, got, tc.want)
			}
		})
	}
}

func TestMatchDispatchTasksDedupesTaskAcrossBuckets(t *testing.T) {
	st := NewStore()
	st.tasks = map[string]*TaskState{
		"t1": {Name: "t1"},
	}
	st.selectorCfg = map[string]config.SelectorConfig{
		"by-ip": {
			Receivers: []string{"r1"},
			Tasks:     []string{"t1"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.1"}},
		},
		"cidr-range": {
			Receivers: []string{"r1"},
			Tasks:     []string{"t1"},
			Source:    &config.SourceSelectorConfig{SrcCIDRs: []string{"10.0.0.0/24"}, SrcPortRanges: []string{"9000-9010"}},
		},
		"default": {Receivers: []string{"r1"}, Tasks: []string{"t1"}},
	}
	if err := st.refreshDispatchSubs(); err != nil {
		t.Fatalf("refresh dispatch: %v", err)
	}
	got := st.matchDispatchTasks("r1", &packet.Packet{Envelope: packet.Envelope{Meta: packet.Meta{Remote: "10.0.0.1:9001"}}})
	if len(got) != 1 || got[0].Name != "t1" {
		t.Fatalf("expected deduped single task, got=%v", taskNames(got))
	}
}

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
	pkt := &packet.Packet{Envelope: packet.Envelope{Payload: append([]byte(nil), payload...), Meta: packet.Meta{Remote: "192.0.2.1:5000"}}}
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

func taskNames(tasks []*TaskState) []string {
	out := make([]string, 0, len(tasks))
	for _, ts := range tasks {
		out = append(out, ts.Name)
	}
	return out
}
