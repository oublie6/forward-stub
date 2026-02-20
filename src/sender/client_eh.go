package sender

import "github.com/panjf2000/gnet/v2"

type clientEH struct{ gnet.BuiltinEventEngine }

func (h *clientEH) OnTraffic(c gnet.Conn) gnet.Action {
	_, _ = c.Next(-1)
	return gnet.None
}
