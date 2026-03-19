package runtime

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"forward-stub/src/config"
)

func TestForwardUDPToUDPWithChannelTaskModel(t *testing.T) {
	recvPort := freeUDPPort(t)
	sendPort := freeUDPPort(t)
	localPort := freeUDPPort(t)

	done := make(chan []byte, 1)
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", sendPort))
	if err != nil {
		t.Fatalf("listen output udp: %v", err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := pc.ReadFrom(buf)
		if err == nil {
			done <- append([]byte(nil), buf[:n]...)
		}
	}()

	st := NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Config{
		Version: 1,
		Logging: config.LoggingConfig{Level: "error"},
		Receivers: map[string]config.ReceiverConfig{
			"r1": {Type: "udp_gnet", Listen: fmt.Sprintf("udp://127.0.0.1:%d", recvPort), Selector: "sel1"},
		},
		Selectors: map[string]config.SelectorConfig{
			"sel1": {DefaultTaskSet: "ts1"},
		},
		TaskSets: map[string][]string{
			"ts1": []string{"t1"},
		},
		Senders: map[string]config.SenderConfig{
			"s1": {Type: "udp_unicast", Remote: fmt.Sprintf("127.0.0.1:%d", sendPort), LocalIP: "127.0.0.1", LocalPort: localPort},
		},
		Tasks: map[string]config.TaskConfig{
			"t1": {
				ExecutionModel: "channel",
				QueueSize:      128,
				Pipelines:      []string{"p"},
				Senders:        []string{"s1"},
			},
		},
		Pipelines: map[string][]config.StageConfig{"p": {}},
	}
	if err := UpdateCache(ctx, st, cfg); err != nil {
		t.Fatalf("update cache: %v", err)
	}
	defer st.StopAll(context.Background())

	payload := []byte("channel-model-forward")
	c, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", recvPort))
	if err != nil {
		t.Fatalf("dial input udp: %v", err)
	}
	defer c.Close()
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write input payload: %v", err)
	}
	select {
	case got := <-done:
		if string(got) != string(payload) {
			t.Fatalf("payload mismatch got=%q want=%q", got, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting forwarded packet")
	}
}
