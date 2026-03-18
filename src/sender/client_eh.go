// client_eh.go 定义 gnet 客户端事件处理器的最小实现。
package sender

import "github.com/panjf2000/gnet/v2"

// clientEH 是供 client_eh.go 使用的包内辅助结构。
type clientEH struct{ gnet.BuiltinEventEngine }

// OnTraffic 负责该函数对应的核心逻辑，详见实现细节。
func (h *clientEH) OnTraffic(c gnet.Conn) gnet.Action {
	_, _ = c.Next(-1)
	return gnet.None
}
