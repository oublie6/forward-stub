// client_eh.go 定义 gnet 客户端事件处理器的最小实现。
package sender

import "github.com/panjf2000/gnet/v2"

type clientEH struct{ gnet.BuiltinEventEngine }

// OnTraffic 丢弃 TCP client 侧收到的数据。
// 当前 TCP sender 只写出 payload，不消费对端响应。
func (h *clientEH) OnTraffic(c gnet.Conn) gnet.Action {
	_, _ = c.Next(-1)
	return gnet.None
}
