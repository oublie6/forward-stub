package runtime

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"
	"forward-stub/src/sender"
	"forward-stub/src/task"
)

// freeTCPPort is a package-local helper used by forward_matrix_test.go.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// freeUDPPort is a package-local helper used by forward_matrix_test.go.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// dialTCPWithRetry is a package-local helper used by forward_matrix_test.go.
func dialTCPWithRetry(addr string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runSingleForward is a package-local helper used by forward_matrix_test.go.
func runSingleForward(t *testing.T, recv config.ReceiverConfig, sendCfg config.SenderConfig, probe func([]byte) error) {
	t.Helper()
	st := NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Config{
		Version:   1,
		Logging:   config.LoggingConfig{Level: "error"},
		Receivers: map[string]config.ReceiverConfig{"r1": recv},
		Senders:   map[string]config.SenderConfig{"s1": sendCfg},
		Selectors: testSelector("r1", "t1"),
		Tasks: map[string]config.TaskConfig{
			"t1": {PoolSize: 1, FastPath: true, Senders: []string{"s1"}},
		},
		Pipelines: map[string][]config.StageConfig{},
	}
	if err := UpdateCache(ctx, st, cfg); err != nil {
		t.Fatalf("update cache: %v", err)
	}
	defer st.StopAll(context.Background())

	if err := probe([]byte("matrix-forward-payload")); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
}

// TestForwardMatrixUDPToUDP_Actual verifies the ForwardMatrixUDPToUDP_Actual behavior for the runtime package.
func TestForwardMatrixUDPToUDP_Actual(t *testing.T) {
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

	runSingleForward(t,
		config.ReceiverConfig{Type: "udp_gnet", Listen: fmt.Sprintf("udp://127.0.0.1:%d", recvPort)},
		config.SenderConfig{Type: "udp_unicast", Remote: fmt.Sprintf("127.0.0.1:%d", sendPort), LocalIP: "127.0.0.1", LocalPort: localPort},
		func(payload []byte) error {
			c, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", recvPort))
			if err != nil {
				return err
			}
			defer c.Close()
			if _, err := c.Write(payload); err != nil {
				return err
			}
			select {
			case got := <-done:
				if string(got) != string(payload) {
					return fmt.Errorf("payload mismatch: got=%q want=%q", string(got), string(payload))
				}
				return nil
			case <-time.After(3 * time.Second):
				return fmt.Errorf("timeout waiting output udp packet")
			}
		},
	)
}

// TestForwardMatrixTCPToTCP_Actual verifies the ForwardMatrixTCPToTCP_Actual behavior for the runtime package.
func TestForwardMatrixTCPToTCP_Actual(t *testing.T) {
	recvPort := freeTCPPort(t)
	sendPort := freeTCPPort(t)

	done := make(chan []byte, 1)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sendPort))
	if err != nil {
		t.Fatalf("listen output tcp: %v", err)
	}
	defer ln.Close()
	go func() {
		_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 2048)
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := conn.Read(buf)
		if err == nil {
			done <- append([]byte(nil), buf[:n]...)
		}
	}()

	runSingleForward(t,
		config.ReceiverConfig{Type: "tcp_gnet", Listen: fmt.Sprintf("tcp://127.0.0.1:%d", recvPort)},
		config.SenderConfig{Type: "tcp_gnet", Remote: fmt.Sprintf("127.0.0.1:%d", sendPort), Concurrency: 1, Frame: "none"},
		func(payload []byte) error {
			conn, err := dialTCPWithRetry(fmt.Sprintf("127.0.0.1:%d", recvPort), 3*time.Second)
			if err != nil {
				return err
			}
			defer conn.Close()
			if _, err := conn.Write(payload); err != nil {
				return err
			}
			select {
			case got := <-done:
				if string(got) != string(payload) {
					return fmt.Errorf("payload mismatch: got=%q want=%q", string(got), string(payload))
				}
				return nil
			case <-time.After(3 * time.Second):
				return fmt.Errorf("timeout waiting output tcp packet")
			}
		},
	)
}

// TestForwardMatrixKafkaAndSFTPSimulated verifies the ForwardMatrixKafkaAndSFTPSimulated behavior for the runtime package.
func TestForwardMatrixKafkaAndSFTPSimulated(t *testing.T) {
	if _, err := buildReceiver("kr", config.ReceiverConfig{Type: "kafka", Listen: "127.0.0.1:9092"}, "error"); err == nil {
		t.Fatalf("expected kafka receiver build to fail when topic is missing")
	}
	if _, err := buildSender("ks", config.SenderConfig{Type: "kafka", Remote: "127.0.0.1:9092"}, "error"); err == nil {
		t.Fatalf("expected kafka sender build to fail when topic is missing")
	}

	if _, err := buildReceiver("sr", config.ReceiverConfig{Type: "sftp", Listen: "127.0.0.1:22", Username: "u", Password: "p", RemoteDir: "/tmp", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"}, "error"); err != nil {
		t.Fatalf("expected sftp receiver build success with valid config: %v", err)
	}
	if _, err := buildSender("ss", config.SenderConfig{Type: "sftp", Remote: "127.0.0.1:1", Username: "u", Password: "p", RemoteDir: "/tmp", HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A"}, "error"); err == nil {
		t.Fatalf("expected sftp sender build to fail without ssh service")
	}
}

// TestBuildSenderRejectsUnknownTCPFrame verifies the BuildSenderRejectsUnknownTCPFrame behavior for the runtime package.
func TestBuildSenderRejectsUnknownTCPFrame(t *testing.T) {
	_, err := buildSender("tcp1", config.SenderConfig{Type: "tcp_gnet", Remote: "127.0.0.1:9", Frame: "bad_frame"}, "error")
	if err == nil {
		t.Fatalf("expected error for unknown frame")
	}
}

// TestSimulatedDispatchAcrossProtocolCombinations verifies the SimulatedDispatchAcrossProtocolCombinations behavior for the runtime package.
func TestSimulatedDispatchAcrossProtocolCombinations(t *testing.T) {
	types := []string{"udp", "tcp", "kafka", "sftp"}
	for _, in := range types {
		for _, out := range types {
			t.Run(in+"_to_"+out, func(t *testing.T) {
				cap := &captureSender{name: out}
				tk := &task.Task{Name: "task", FastPath: true, Senders: []sender.Sender{cap}}
				if err := tk.Start(); err != nil {
					t.Fatalf("task start: %v", err)
				}
				defer tk.StopGraceful()

				st := NewStore()
				st.setDispatchSubs(testDispatchSnapshot(in, &TaskState{Name: "task", T: tk}))
				pkt := &packet.Packet{Envelope: packet.Envelope{Payload: []byte(in + "->" + out)}}
				dispatch(context.Background(), st, in, pkt)
				if got := string(cap.Last()); got != in+"->"+out {
					t.Fatalf("dispatch mismatch got=%q", got)
				}
			})
		}
	}
}
