package runtime

import (
	"fmt"

	"forward-stub/src/config"
	"forward-stub/src/receiver"
	"forward-stub/src/sender"
)

func buildReceiver(name string, rc config.ReceiverConfig, gnetLogLevel string) (receiver.Receiver, error) {
	multicore := config.DefaultReceiverMulticore
	if rc.Multicore != nil {
		multicore = *rc.Multicore
	}
	numEventLoop := rc.NumEventLoop
	if numEventLoop <= 0 {
		numEventLoop = config.DefaultReceiverNumEventLoop
	}
	switch rc.Type {
	case "udp_gnet":
		return receiver.NewGnetUDP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, gnetLogLevel, rc.MatchKey)
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
		return receiver.NewGnetTCP(name, rc.Listen, multicore, numEventLoop, rc.ReadBufferCap, rc.SocketRecvBuffer, fr, gnetLogLevel, rc.MatchKey)
	case "kafka":
		return receiver.NewKafkaReceiver(name, rc)
	case "sftp":
		return receiver.NewSFTPReceiver(name, rc)
	default:
		return nil, fmt.Errorf("receiver %s unknown type %s", name, rc.Type)
	}
}

func buildSender(name string, sc config.SenderConfig, gnetLogLevel string) (sender.Sender, error) {
	conc := sc.Concurrency
	if conc <= 0 {
		conc = config.DefaultSenderConcurrency
	}
	switch sc.Type {
	case "udp_unicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_unicast requires local_port", name)
		}
		return sender.NewUDPUnicastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.SocketSendBuffer, conc)
	case "udp_multicast":
		if sc.LocalPort <= 0 {
			return nil, fmt.Errorf("sender %s udp_multicast requires local_port", name)
		}
		return sender.NewUDPMulticastSender(name, sc.LocalIP, sc.LocalPort, sc.Remote, sc.Iface, sc.TTL, sc.Loop, sc.SocketSendBuffer, conc)
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
		return sender.NewGnetTCPSender(name, sc.Remote, with, conc, sc.SocketSendBuffer, gnetLogLevel)
	case "kafka":
		return sender.NewKafkaSender(name, sc)
	case "sftp":
		return sender.NewSFTPSender(name, sc)
	default:
		return nil, fmt.Errorf("sender %s unknown type %s", name, sc.Type)
	}
}
