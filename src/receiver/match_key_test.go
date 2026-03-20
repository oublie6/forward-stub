package receiver

import (
	"net"
	"testing"

	"forward-stub/src/config"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestCompileUDPMatchKeyBuilderSupportsDefaultAndConfiguredModes(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 9000}
	localAddr := &net.UDPAddr{IP: net.ParseIP("192.168.0.10"), Port: 19000}

	tests := []struct {
		name     string
		cfg      config.ReceiverMatchKeyConfig
		wantKey  string
		wantMode string
	}{
		{name: "compat_default", cfg: config.ReceiverMatchKeyConfig{}, wantKey: "udp|src_addr=10.0.0.1:9000", wantMode: receiverMatchKeyModeCompatDefault},
		{name: "remote_addr", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_addr"}, wantKey: "udp|remote_addr=10.0.0.1:9000", wantMode: "remote_addr"},
		{name: "remote_ip", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_ip"}, wantKey: "udp|remote_ip=10.0.0.1", wantMode: "remote_ip"},
		{name: "local_addr", cfg: config.ReceiverMatchKeyConfig{Mode: "local_addr"}, wantKey: "udp|local_addr=192.168.0.10:19000", wantMode: "local_addr"},
		{name: "local_ip", cfg: config.ReceiverMatchKeyConfig{Mode: "local_ip"}, wantKey: "udp|local_ip=192.168.0.10", wantMode: "local_ip"},
		{name: "fixed", cfg: config.ReceiverMatchKeyConfig{Mode: "fixed", FixedValue: "ingress-a"}, wantKey: "udp|fixed=ingress-a", wantMode: "fixed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, mode, err := compileUDPMatchKeyBuilder(tt.cfg)
			if err != nil {
				t.Fatalf("compile builder: %v", err)
			}
			if mode != tt.wantMode {
				t.Fatalf("unexpected mode: got=%s want=%s", mode, tt.wantMode)
			}
			if got := builder("10.0.0.1:9000", "192.168.0.10:19000", remoteAddr, localAddr); got != tt.wantKey {
				t.Fatalf("unexpected match key: got=%s want=%s", got, tt.wantKey)
			}
		})
	}
}

func TestCompileTCPMatchKeyBuilderSupportsDefaultAndConfiguredModes(t *testing.T) {
	cs := &connState{
		remote:     "10.0.0.2:9001",
		local:      "192.168.0.20:19001",
		remoteAddr: &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 9001},
		localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.0.20"), Port: 19001},
	}

	tests := []struct {
		name     string
		cfg      config.ReceiverMatchKeyConfig
		wantKey  string
		wantMode string
	}{
		{name: "compat_default", cfg: config.ReceiverMatchKeyConfig{}, wantKey: "tcp|src_addr=10.0.0.2:9001", wantMode: receiverMatchKeyModeCompatDefault},
		{name: "remote_addr", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_addr"}, wantKey: "tcp|remote_addr=10.0.0.2:9001", wantMode: "remote_addr"},
		{name: "remote_ip", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_ip"}, wantKey: "tcp|remote_ip=10.0.0.2", wantMode: "remote_ip"},
		{name: "local_addr", cfg: config.ReceiverMatchKeyConfig{Mode: "local_addr"}, wantKey: "tcp|local_addr=192.168.0.20:19001", wantMode: "local_addr"},
		{name: "local_port", cfg: config.ReceiverMatchKeyConfig{Mode: "local_port"}, wantKey: "tcp|local_port=19001", wantMode: "local_port"},
		{name: "fixed", cfg: config.ReceiverMatchKeyConfig{Mode: "fixed", FixedValue: "tcp-a"}, wantKey: "tcp|fixed=tcp-a", wantMode: "fixed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, mode, err := compileTCPMatchKeyBuilder(tt.cfg)
			if err != nil {
				t.Fatalf("compile builder: %v", err)
			}
			if mode != tt.wantMode {
				t.Fatalf("unexpected mode: got=%s want=%s", mode, tt.wantMode)
			}
			if got := builder(cs); got != tt.wantKey {
				t.Fatalf("unexpected match key: got=%s want=%s", got, tt.wantKey)
			}
		})
	}
}

func TestCompileKafkaMatchKeyBuilderSupportsDefaultAndConfiguredModes(t *testing.T) {
	rec := &kgo.Record{Topic: "orders", Partition: 3}
	tests := []struct {
		name     string
		cfg      config.ReceiverMatchKeyConfig
		wantKey  string
		wantMode string
	}{
		{name: "compat_default", cfg: config.ReceiverMatchKeyConfig{}, wantKey: "kafka|topic=orders|partition=3", wantMode: receiverMatchKeyModeCompatDefault},
		{name: "topic", cfg: config.ReceiverMatchKeyConfig{Mode: "topic"}, wantKey: "kafka|topic=orders", wantMode: "topic"},
		{name: "topic_partition", cfg: config.ReceiverMatchKeyConfig{Mode: "topic_partition"}, wantKey: "kafka|topic_partition=orders|3", wantMode: "topic_partition"},
		{name: "fixed", cfg: config.ReceiverMatchKeyConfig{Mode: "fixed", FixedValue: "orders-ingress"}, wantKey: "kafka|fixed=orders-ingress", wantMode: "fixed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, mode, err := compileKafkaMatchKeyBuilder(tt.cfg, "orders")
			if err != nil {
				t.Fatalf("compile builder: %v", err)
			}
			if mode != tt.wantMode {
				t.Fatalf("unexpected mode: got=%s want=%s", mode, tt.wantMode)
			}
			if got := builder(rec); got != tt.wantKey {
				t.Fatalf("unexpected match key: got=%s want=%s", got, tt.wantKey)
			}
		})
	}
}

func TestCompileSFTPMatchKeyBuilderSupportsDefaultAndConfiguredModes(t *testing.T) {
	filePath := "/input/orders.csv"
	tests := []struct {
		name     string
		cfg      config.ReceiverMatchKeyConfig
		wantKey  string
		wantMode string
	}{
		{name: "compat_default", cfg: config.ReceiverMatchKeyConfig{}, wantKey: "sftp|remote_dir=/input|file_name=orders.csv", wantMode: receiverMatchKeyModeCompatDefault},
		{name: "remote_path", cfg: config.ReceiverMatchKeyConfig{Mode: "remote_path"}, wantKey: "sftp|remote_path=/input/orders.csv", wantMode: "remote_path"},
		{name: "filename", cfg: config.ReceiverMatchKeyConfig{Mode: "filename"}, wantKey: "sftp|filename=orders.csv", wantMode: "filename"},
		{name: "fixed", cfg: config.ReceiverMatchKeyConfig{Mode: "fixed", FixedValue: "daily-batch"}, wantKey: "sftp|fixed=daily-batch", wantMode: "fixed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, mode, err := compileSFTPMatchKeyBuilder(tt.cfg, "/input")
			if err != nil {
				t.Fatalf("compile builder: %v", err)
			}
			if mode != tt.wantMode {
				t.Fatalf("unexpected mode: got=%s want=%s", mode, tt.wantMode)
			}
			if got := builder(filePath); got != tt.wantKey {
				t.Fatalf("unexpected match key: got=%s want=%s", got, tt.wantKey)
			}
		})
	}
}

func TestReceiverConstructorsExposeCompiledMatchKeyMode(t *testing.T) {
	udp, err := NewGnetUDP("udp", ":9000", true, 1, 0, 0, "info", config.ReceiverMatchKeyConfig{Mode: "remote_ip"})
	if err != nil {
		t.Fatalf("new udp: %v", err)
	}
	if udp.MatchKeyMode() != "remote_ip" {
		t.Fatalf("unexpected udp mode: %s", udp.MatchKeyMode())
	}

	tcp, err := NewGnetTCP("tcp", ":9001", true, 1, 0, 0, nil, "info", config.ReceiverMatchKeyConfig{Mode: "local_port"})
	if err != nil {
		t.Fatalf("new tcp: %v", err)
	}
	if tcp.MatchKeyMode() != "local_port" {
		t.Fatalf("unexpected tcp mode: %s", tcp.MatchKeyMode())
	}

	kafkaReceiver, err := NewKafkaReceiver("kafka", config.ReceiverConfig{
		Type:      "kafka",
		Listen:    "127.0.0.1:9092",
		Topic:     "orders",
		GroupID:   "group-1",
		Balancers: config.DefaultKafkaReceiverBalancers,
		MatchKey:  config.ReceiverMatchKeyConfig{Mode: "topic"},
	})
	if err != nil {
		t.Fatalf("new kafka: %v", err)
	}
	if kafkaReceiver.MatchKeyMode() != "topic" {
		t.Fatalf("unexpected kafka mode: %s", kafkaReceiver.MatchKeyMode())
	}
	_ = kafkaReceiver.Stop(t.Context())

	sftpReceiver, err := NewSFTPReceiver("sftp", config.ReceiverConfig{
		Type:               "sftp",
		Listen:             "127.0.0.1:22",
		Username:           "u",
		Password:           "p",
		RemoteDir:          "/input",
		HostKeyFingerprint: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A",
		MatchKey:           config.ReceiverMatchKeyConfig{Mode: "filename"},
	})
	if err != nil {
		t.Fatalf("new sftp: %v", err)
	}
	if sftpReceiver.MatchKeyMode() != "filename" {
		t.Fatalf("unexpected sftp mode: %s", sftpReceiver.MatchKeyMode())
	}
}
