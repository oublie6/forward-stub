package receiver

import "strings"

func normalizeGnetListen(proto, addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return proto + "://" + addr
}
