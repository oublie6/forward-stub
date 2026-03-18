package runtime

import (
	"net/netip"

	"forward-stub/src/config"
)

func testDispatchSnapshot(receiver string, tasks ...*TaskState) map[string]*ReceiverSelectorDispatchState {
	return map[string]*ReceiverSelectorDispatchState{
		receiver: {
			DefaultTasks:  append([]*TaskState(nil), tasks...),
			ExactAddrPort: map[netip.AddrPort][]*TaskState{},
			ByIP:          map[netip.Addr][]*TaskState{},
			ByPort:        map[uint16][]*TaskState{},
		},
	}
}

func testSelector(receiver string, tasks ...string) map[string]config.SelectorConfig {
	return map[string]config.SelectorConfig{
		"sel": {
			Receivers: []string{receiver},
			Tasks:     tasks,
		},
	}
}
