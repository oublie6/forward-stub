package runtime

import (
	"fmt"

	"forward-stub/src/config"
	"forward-stub/src/receiver"
	"forward-stub/src/sender"
)

func buildReceiver(name string, rc config.ReceiverConfig, gnetLogLevel string) (receiver.Receiver, error) {
	multicore := config.BoolValue(rc.Multicore)
	switch rc.Type {
	case "udp_gnet":
		return receiver.NewGnetUDP(name, rc.Listen, multicore, rc.NumEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, gnetLogLevel, rc.MatchKey)
	case "tcp_gnet":
		var fr receiver.Framer
		switch rc.Frame {
		case "":
			fr = nil
		case "u16be":
			fr = receiver.U16BEFramer{}
		default:
			return nil, fmt.Errorf("receiver %s unknown frame %s", name, rc.Frame)
		}
		return receiver.NewGnetTCP(name, rc.Listen, multicore, rc.NumEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, fr, gnetLogLevel, rc.MatchKey)
	case "local_timer":
		return receiver.NewLocalTimerReceiver(name, rc)
	case "kafka":
		return receiver.NewKafkaReceiver(name, rc)
	case "sftp":
		return receiver.NewSFTPReceiver(name, rc)
	case "oss":
		return receiver.NewOSSReceiver(name, rc)
	case "dds_skydds":
		return receiver.NewSkyDDSReceiver(name, rc)
	default:
		return nil, fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
}

func buildSender(name string, sc config.SenderConfig, gnetLogLevel string) (sender.Sender, error) {
	switch sc.Type {
	case "udp_unicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_unicast requires local_port", name)
		}
		return sender.NewUDPUnicastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.SocketSendBuffer, sc.Concurrency)
	case "udp_multicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_multicast requires local_port", name)
		}
		return sender.NewUDPMulticastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.Iface, sc.TTL, sc.Loop, sc.SocketSendBuffer, sc.Concurrency)
	case "tcp_gnet":
		with := false
		switch sc.Frame {
		case "":
			with = false
		case "u16be":
			with = true
		default:
			return nil, fmt.Errorf("sender %s unknown frame %s", name, sc.Frame)
		}
		return sender.NewGnetTCPSender(name, sc.Remote, with, sc.Concurrency, sc.SocketSendBuffer, gnetLogLevel)
	case "kafka":
		return sender.NewKafkaSender(name, sc)
	case "sftp":
		return sender.NewSFTPSender(name, sc)
	case "oss":
		return sender.NewOSSSender(name, sc)
	case "dds_skydds":
		return sender.NewSkyDDSSender(name, sc)
	default:
		return nil, fmt.Errorf("sender %s unknown type %s", name, sc.Type)
	}
}
