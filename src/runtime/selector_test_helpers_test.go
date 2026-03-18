package runtime

import (
	"net/netip"

	"forward-stub/src/config"
)

// testDispatchSnapshot is a package-local helper used by selector_test_helpers_test.go.
func testDispatchSnapshot(receiver string, tasks ...*TaskState) map[string]*ReceiverSelectorDispatchState {
	return map[string]*ReceiverSelectorDispatchState{
		receiver: {
			DefaultTasks:   append([]*TaskState(nil), tasks...),
			ExactAddrPort:  map[uint64][]*TaskState{},
			ByIP:           map[uint32][]*TaskState{},
			ByPort:         map[uint16][]*TaskState{},
			ExactAddrPort6: map[netip.AddrPort][]*TaskState{},
			ByIP6:          map[netip.Addr][]*TaskState{},
		},
	}
}

// testSelector is a package-local helper used by selector_test_helpers_test.go.
func testSelector(receiver string, tasks ...string) map[string]config.SelectorConfig {
	return map[string]config.SelectorConfig{
		"sel": {
			Receivers: []string{receiver},
			Tasks:     tasks,
		},
	}
}
