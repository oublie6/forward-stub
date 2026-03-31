package kafkautil

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/packet"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
)

func SplitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func IntDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func DurationOrDefault(value, fallback string) (time.Duration, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		raw = fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be > 0")
	}
	return d, nil
}

func RequiredAcks(acks int) kgo.Acks {
	switch acks {
	case 0:
		return kgo.NoAck()
	case 1:
		return kgo.LeaderAck()
	default:
		return kgo.AllISRAcks()
	}
}

func CompressionCodec(v string, level int) (kgo.CompressionCodec, bool, error) {
	withLevel := func(codec kgo.CompressionCodec) kgo.CompressionCodec {
		if level == 0 {
			return codec
		}
		return codec.WithLevel(level)
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return kgo.NoCompression(), false, nil
	case "gzip":
		return withLevel(kgo.GzipCompression()), true, nil
	case "snappy":
		return kgo.SnappyCompression(), true, nil
	case "lz4":
		return withLevel(kgo.Lz4Compression()), true, nil
	case "zstd":
		return withLevel(kgo.ZstdCompression()), true, nil
	default:
		return kgo.NoCompression(), false, fmt.Errorf("kafka compression %s unsupported", v)
	}
}

func Partitioner(v string) (kgo.Partitioner, error) {
	switch strings.TrimSpace(v) {
	case "", "sticky":
		return kgo.StickyPartitioner(), nil
	case "round_robin":
		return kgo.RoundRobinPartitioner(), nil
	case "hash_key":
		return kgo.StickyKeyPartitioner(nil), nil
	default:
		return nil, fmt.Errorf("kafka partitioner %s unsupported", v)
	}
}

func RecordKey(fixedKey []byte, source string, p *packet.Packet) []byte {
	if len(fixedKey) > 0 {
		return append([]byte(nil), fixedKey...)
	}
	if p == nil {
		return nil
	}
	switch source {
	case "":
		return nil
	case "payload":
		return append([]byte(nil), p.Payload...)
	case "match_key":
		return []byte(p.Meta.MatchKey)
	case "remote":
		return []byte(p.Meta.Remote)
	case "local":
		return []byte(p.Meta.Local)
	case "file_name":
		return []byte(p.Meta.FileName)
	case "file_path":
		return []byte(p.Meta.FilePath)
	case "transfer_id":
		return []byte(p.Meta.TransferID)
	case "route_sender":
		return []byte(p.Meta.RouteSender)
	default:
		return nil
	}
}

func IsolationLevel(v string) (kgo.IsolationLevel, error) {
	switch strings.TrimSpace(v) {
	case "", "read_uncommitted":
		return kgo.ReadUncommitted(), nil
	case "read_committed":
		return kgo.ReadCommitted(), nil
	default:
		return kgo.ReadUncommitted(), fmt.Errorf("kafka isolation_level %s unsupported", v)
	}
}

func GroupBalancers(values []string) ([]kgo.GroupBalancer, error) {
	if len(values) == 0 {
		values = config.DefaultKafkaReceiverBalancers
	}
	out := make([]kgo.GroupBalancer, 0, len(values))
	for _, value := range values {
		switch strings.TrimSpace(value) {
		case "range":
			out = append(out, kgo.RangeBalancer())
		case "round_robin":
			out = append(out, kgo.RoundRobinBalancer())
		case "cooperative_sticky":
			out = append(out, kgo.CooperativeStickyBalancer())
		default:
			return nil, fmt.Errorf("kafka balancer %s unsupported", value)
		}
	}
	return out, nil
}

func BuildSASLMechanism(mechanism, username, password string) (sasl.Mechanism, error) {
	mech := strings.ToUpper(strings.TrimSpace(mechanism))
	u := strings.TrimSpace(username)
	p := strings.TrimSpace(password)
	if mech == "" && (u != "" || p != "") {
		mech = "PLAIN"
	}
	if mech == "" {
		return nil, nil
	}
	if u == "" || p == "" {
		return nil, fmt.Errorf("kafka sasl %s requires username and password", mech)
	}
	if mech != "PLAIN" {
		return nil, fmt.Errorf("kafka sasl mechanism %s unsupported, only PLAIN is supported", mech)
	}
	return plainMechanism{username: u, password: p}, nil
}

type plainMechanism struct {
	username string
	password string
}

func (m plainMechanism) Name() string { return "PLAIN" }

func (m plainMechanism) Authenticate(_ context.Context, _ string) (sasl.Session, []byte, error) {
	msg := []byte("\x00" + m.username + "\x00" + m.password)
	return plainSession{}, msg, nil
}

type plainSession struct{}

func (plainSession) Challenge(_ []byte) (bool, []byte, error) {
	return true, nil, nil
}
