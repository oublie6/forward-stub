package runtime

import (
	"context"
	"testing"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

func makeTaskScopedStagePacket(payload string) *packet.Packet {
	return &packet.Packet{
		Envelope: packet.Envelope{
			Kind:    packet.PayloadKindStream,
			Payload: []byte(payload),
			Meta: packet.Meta{
				Proto:        packet.ProtoUDP,
				ReceiverName: "receiver_a",
				MatchKey:     "match_key_same",
			},
		},
	}
}

// TestAddTaskBuildsTaskScopedStatefulStages 验证带缓冲状态的 stage 不会在多个 task 之间共享实例。
func TestAddTaskBuildsTaskScopedStatefulStages(t *testing.T) {
	st := NewStore()
	st.senders["s1"] = &SenderState{Name: "s1", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{testNamedSender: testNamedSender{name: "s1"}}}
	st.senders["s2"] = &SenderState{Name: "s2", Cfg: config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:12345"}, S: &captureSender{testNamedSender: testNamedSender{name: "s2"}}}

	cfgs := map[string][]config.StageConfig{
		"p1": {
			{Type: "stream_packets_to_file_segments", SegmentSize: 4, ChunkSize: 2, Path: "/out", FilePrefix: "seg", TimeLayout: "20060102"},
		},
	}
	compiled, err := CompilePipelines(cfgs)
	if err != nil {
		t.Fatalf("compile pipelines: %v", err)
	}
	st.pipelines = compiled
	st.pipelineCfg = cfgs

	if err := st.addTask("t1", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s1"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add task t1: %v", err)
	}
	if err := st.addTask("t2", config.TaskConfig{Pipelines: []string{"p1"}, Senders: []string{"s2"}, ExecutionModel: "fastpath"}, config.LoggingConfig{}, nil); err != nil {
		t.Fatalf("add task t2: %v", err)
	}
	defer st.tasks["t1"].T.StopGraceful()
	defer st.tasks["t2"].T.StopGraceful()

	s1 := st.senders["s1"].S.(*captureSender)
	s2 := st.senders["s2"].S.(*captureSender)

	st.tasks["t1"].T.Handle(context.Background(), makeTaskScopedStagePacket("ab"))
	st.tasks["t2"].T.Handle(context.Background(), makeTaskScopedStagePacket("cd"))
	s1.mu.Lock()
	s1Count := len(s1.payload)
	s1.mu.Unlock()
	s2.mu.Lock()
	s2Count := len(s2.payload)
	s2.mu.Unlock()
	if s1Count != 0 || s2Count != 0 {
		t.Fatalf("task-scoped stage should not share pending buffer across tasks: s1=%d s2=%d", s1Count, s2Count)
	}

	st.tasks["t1"].T.Handle(context.Background(), makeTaskScopedStagePacket("ef"))
	st.tasks["t2"].T.Handle(context.Background(), makeTaskScopedStagePacket("gh"))
	s1.mu.Lock()
	s1Count = len(s1.payload)
	s1Payload := ""
	if s1Count >= 2 {
		s1Payload = string(s1.payload[0]) + string(s1.payload[1])
	}
	s1.mu.Unlock()
	s2.mu.Lock()
	s2Count = len(s2.payload)
	s2Payload := ""
	if s2Count >= 2 {
		s2Payload = string(s2.payload[0]) + string(s2.payload[1])
	}
	s2.mu.Unlock()
	if s1Count != 2 || s2Count != 2 {
		t.Fatalf("each task should flush its own segment independently: s1=%d s2=%d", s1Count, s2Count)
	}
	if s1Payload != "abef" {
		t.Fatalf("task t1 payload mixed with other task: %q", s1Payload)
	}
	if s2Payload != "cdgh" {
		t.Fatalf("task t2 payload mixed with other task: %q", s2Payload)
	}
}
