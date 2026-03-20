package receiver

import (
	"net"
	"path"
	"strconv"
	"strings"

	"forward-stub/src/config"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	receiverMatchKeyModeCompatDefault = "compat_default"

	udpMatchKeyModeLegacyDefault = ""
	udpMatchKeyModeRemoteAddr    = "remote_addr"
	udpMatchKeyModeRemoteIP      = "remote_ip"
	udpMatchKeyModeLocalAddr     = "local_addr"
	udpMatchKeyModeLocalIP       = "local_ip"
	udpMatchKeyModeFixed         = "fixed"

	tcpMatchKeyModeLegacyDefault = ""
	tcpMatchKeyModeRemoteAddr    = "remote_addr"
	tcpMatchKeyModeRemoteIP      = "remote_ip"
	tcpMatchKeyModeLocalAddr     = "local_addr"
	tcpMatchKeyModeLocalPort     = "local_port"
	tcpMatchKeyModeFixed         = "fixed"

	kafkaMatchKeyModeLegacyDefault = ""
	kafkaMatchKeyModeTopic         = "topic"
	kafkaMatchKeyModeTopicPart     = "topic_partition"
	kafkaMatchKeyModeFixed         = "fixed"

	sftpMatchKeyModeLegacyDefault = ""
	sftpMatchKeyModeRemotePath    = "remote_path"
	sftpMatchKeyModeFilename      = "filename"
	sftpMatchKeyModeFixed         = "fixed"
)

type udpMatchKeyBuilder func(remote, local string, remoteAddr, localAddr net.Addr) string

type tcpMatchKeyBuilder func(cs *connState) string

type kafkaMatchKeyBuilder func(rec *kgo.Record) string

type sftpMatchKeyBuilder func(filePath string) string

var matchKeyEscaper = strings.NewReplacer(
	`\\`, `\\\\`,
	`|`, `\|`,
	`=`, `\=`,
)

// MatchKeyMode 返回 receiver 当前已编译生效的 match key 模式。
// 该方法只用于观测、测试与热重载诊断，不参与收包热路径。
type MatchKeyMode interface {
	MatchKeyMode() string
}

func buildSingleFieldMatchKey(proto, fieldName, value string) string {
	var b strings.Builder
	b.Grow(len(proto) + len(fieldName) + len(value) + 2)
	b.WriteString(proto)
	b.WriteByte('|')
	b.WriteString(fieldName)
	b.WriteByte('=')
	b.WriteString(matchKeyEscaper.Replace(value))
	return b.String()
}

func buildTwoFieldMatchKey(proto, fieldNameA, valueA, fieldNameB, valueB string) string {
	var b strings.Builder
	b.Grow(len(proto) + len(fieldNameA) + len(valueA) + len(fieldNameB) + len(valueB) + 4)
	b.WriteString(proto)
	b.WriteByte('|')
	b.WriteString(fieldNameA)
	b.WriteByte('=')
	b.WriteString(matchKeyEscaper.Replace(valueA))
	b.WriteByte('|')
	b.WriteString(fieldNameB)
	b.WriteByte('=')
	b.WriteString(matchKeyEscaper.Replace(valueB))
	return b.String()
}

func precomputeSingleFieldMatchKey(proto, fieldName, value string) string {
	return buildSingleFieldMatchKey(proto, fieldName, value)
}

func appendRawSingleFieldPrefix(proto, fieldName, escapedValue string) string {
	var b strings.Builder
	b.Grow(len(proto) + len(fieldName) + len(escapedValue) + 2)
	b.WriteString(proto)
	b.WriteByte('|')
	b.WriteString(fieldName)
	b.WriteByte('=')
	b.WriteString(escapedValue)
	return b.String()
}

func matchKeyHostIP(addr net.Addr, fallback string) string {
	if addr == nil {
		return matchKeyHostFromString(fallback)
	}
	switch v := addr.(type) {
	case *net.UDPAddr:
		if v.IP != nil {
			return v.IP.String()
		}
	case *net.TCPAddr:
		if v.IP != nil {
			return v.IP.String()
		}
	}
	return matchKeyHostFromString(fallback)
}

func matchKeyPort(addr net.Addr, fallback string) string {
	if addr == nil {
		return matchKeyPortFromString(fallback)
	}
	switch v := addr.(type) {
	case *net.UDPAddr:
		return strconv.Itoa(v.Port)
	case *net.TCPAddr:
		return strconv.Itoa(v.Port)
	}
	return matchKeyPortFromString(fallback)
}

func matchKeyHostFromString(addr string) string {
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func matchKeyPortFromString(addr string) string {
	if addr == "" {
		return ""
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}

func compileUDPMatchKeyBuilder(cfg config.ReceiverMatchKeyConfig) (udpMatchKeyBuilder, string, error) {
	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case udpMatchKeyModeLegacyDefault:
		return func(remote, _ string, _ net.Addr, _ net.Addr) string {
			return buildSingleFieldMatchKey("udp", "src_addr", remote)
		}, receiverMatchKeyModeCompatDefault, nil
	case udpMatchKeyModeRemoteAddr:
		return func(remote, _ string, _ net.Addr, _ net.Addr) string {
			return buildSingleFieldMatchKey("udp", "remote_addr", remote)
		}, mode, nil
	case udpMatchKeyModeRemoteIP:
		return func(remote, _ string, remoteAddr net.Addr, _ net.Addr) string {
			return buildSingleFieldMatchKey("udp", "remote_ip", matchKeyHostIP(remoteAddr, remote))
		}, mode, nil
	case udpMatchKeyModeLocalAddr:
		return func(_, local string, _ net.Addr, _ net.Addr) string {
			return buildSingleFieldMatchKey("udp", "local_addr", local)
		}, mode, nil
	case udpMatchKeyModeLocalIP:
		return func(_, local string, _ net.Addr, localAddr net.Addr) string {
			return buildSingleFieldMatchKey("udp", "local_ip", matchKeyHostIP(localAddr, local))
		}, mode, nil
	case udpMatchKeyModeFixed:
		fixed := precomputeSingleFieldMatchKey("udp", "fixed", cfg.FixedValue)
		return func(_, _ string, _ net.Addr, _ net.Addr) string { return fixed }, mode, nil
	default:
		return nil, "", config.ErrUnsupportedReceiverMatchKeyMode("udp_gnet", mode)
	}
}

func compileTCPMatchKeyBuilder(cfg config.ReceiverMatchKeyConfig) (tcpMatchKeyBuilder, string, error) {
	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case tcpMatchKeyModeLegacyDefault:
		return func(cs *connState) string {
			return buildSingleFieldMatchKey("tcp", "src_addr", cs.remote)
		}, receiverMatchKeyModeCompatDefault, nil
	case tcpMatchKeyModeRemoteAddr:
		return func(cs *connState) string {
			return buildSingleFieldMatchKey("tcp", "remote_addr", cs.remote)
		}, mode, nil
	case tcpMatchKeyModeRemoteIP:
		return func(cs *connState) string {
			return buildSingleFieldMatchKey("tcp", "remote_ip", matchKeyHostIP(cs.remoteAddr, cs.remote))
		}, mode, nil
	case tcpMatchKeyModeLocalAddr:
		return func(cs *connState) string {
			return buildSingleFieldMatchKey("tcp", "local_addr", cs.local)
		}, mode, nil
	case tcpMatchKeyModeLocalPort:
		return func(cs *connState) string {
			return buildSingleFieldMatchKey("tcp", "local_port", matchKeyPort(cs.localAddr, cs.local))
		}, mode, nil
	case tcpMatchKeyModeFixed:
		fixed := precomputeSingleFieldMatchKey("tcp", "fixed", cfg.FixedValue)
		return func(_ *connState) string { return fixed }, mode, nil
	default:
		return nil, "", config.ErrUnsupportedReceiverMatchKeyMode("tcp_gnet", mode)
	}
}

func compileKafkaMatchKeyBuilder(cfg config.ReceiverMatchKeyConfig, topic string) (kafkaMatchKeyBuilder, string, error) {
	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case kafkaMatchKeyModeLegacyDefault:
		topicValue := matchKeyEscaper.Replace(topic)
		prefix := appendRawSingleFieldPrefix("kafka", "topic", topicValue) + "|partition="
		return func(rec *kgo.Record) string {
			return prefix + strconv.Itoa(int(rec.Partition))
		}, receiverMatchKeyModeCompatDefault, nil
	case kafkaMatchKeyModeTopic:
		key := precomputeSingleFieldMatchKey("kafka", "topic", topic)
		return func(_ *kgo.Record) string { return key }, mode, nil
	case kafkaMatchKeyModeTopicPart:
		topicValue := matchKeyEscaper.Replace(topic)
		prefix := appendRawSingleFieldPrefix("kafka", "topic_partition", topicValue) + "|"
		return func(rec *kgo.Record) string {
			return prefix + strconv.Itoa(int(rec.Partition))
		}, mode, nil
	case kafkaMatchKeyModeFixed:
		key := precomputeSingleFieldMatchKey("kafka", "fixed", cfg.FixedValue)
		return func(_ *kgo.Record) string { return key }, mode, nil
	default:
		return nil, "", config.ErrUnsupportedReceiverMatchKeyMode("kafka", mode)
	}
}

func compileSFTPMatchKeyBuilder(cfg config.ReceiverMatchKeyConfig, remoteDir string) (sftpMatchKeyBuilder, string, error) {
	mode := strings.TrimSpace(cfg.Mode)
	switch mode {
	case sftpMatchKeyModeLegacyDefault:
		remoteDirValue := strings.TrimSpace(remoteDir)
		return func(filePath string) string {
			return buildTwoFieldMatchKey("sftp", "remote_dir", remoteDirValue, "file_name", path.Base(filePath))
		}, receiverMatchKeyModeCompatDefault, nil
	case sftpMatchKeyModeRemotePath:
		return func(filePath string) string {
			return buildSingleFieldMatchKey("sftp", "remote_path", filePath)
		}, mode, nil
	case sftpMatchKeyModeFilename:
		return func(filePath string) string {
			return buildSingleFieldMatchKey("sftp", "filename", path.Base(filePath))
		}, mode, nil
	case sftpMatchKeyModeFixed:
		key := precomputeSingleFieldMatchKey("sftp", "fixed", cfg.FixedValue)
		return func(_ string) string { return key }, mode, nil
	default:
		return nil, "", config.ErrUnsupportedReceiverMatchKeyMode("sftp", mode)
	}
}
