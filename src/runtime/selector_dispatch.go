package runtime

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

type ReceiverSelectorDispatchState struct {
	ExactAddrPort   map[netip.AddrPort][]*TaskState
	ByIP            map[netip.Addr][]*TaskState
	ByPort          map[uint16][]*TaskState
	CIDRBuckets     []CIDRDispatchBucket
	PortBuckets     []PortRangeDispatchBucket
	CIDRPortBuckets []CIDRPortDispatchBucket
	DefaultTasks    []*TaskState
}

type CIDRDispatchBucket struct {
	Prefix netip.Prefix
	Tasks  []*TaskState
}

type PortRangeDispatchBucket struct {
	Start uint16
	End   uint16
	Tasks []*TaskState
}

type CIDRPortDispatchBucket struct {
	Prefix netip.Prefix
	Start  uint16
	End    uint16
	Tasks  []*TaskState
}

func buildSelectorDispatchSnapshot(selectors map[string]config.SelectorConfig, tasks map[string]*TaskState) (map[string]*ReceiverSelectorDispatchState, error) {
	snapshot := make(map[string]*ReceiverSelectorDispatchState)
	for selectorName, sc := range selectors {
		selectorTasks, err := selectorTasksForDispatch(selectorName, sc, tasks)
		if err != nil {
			return nil, err
		}
		for _, receiverName := range sc.Receivers {
			state := snapshot[receiverName]
			if state == nil {
				state = &ReceiverSelectorDispatchState{
					ExactAddrPort: make(map[netip.AddrPort][]*TaskState),
					ByIP:          make(map[netip.Addr][]*TaskState),
					ByPort:        make(map[uint16][]*TaskState),
				}
				snapshot[receiverName] = state
			}
			if err := compileSelectorIntoDispatchState(state, sc, selectorTasks); err != nil {
				return nil, fmt.Errorf("selector %s receiver %s compile error: %w", selectorName, receiverName, err)
			}
		}
	}
	return snapshot, nil
}

func selectorTasksForDispatch(_ string, sc config.SelectorConfig, tasks map[string]*TaskState) ([]*TaskState, error) {
	out := make([]*TaskState, 0, len(sc.Tasks))
	for _, taskName := range sc.Tasks {
		ts := tasks[taskName]
		if ts == nil {
			continue
		}
		out = append(out, ts)
	}
	return out, nil
}

func compileSelectorIntoDispatchState(state *ReceiverSelectorDispatchState, sc config.SelectorConfig, selectorTasks []*TaskState) error {
	if len(selectorTasks) == 0 {
		return nil
	}
	if sc.Source == nil {
		state.DefaultTasks = mergeTaskStateSliceUnique(state.DefaultTasks, selectorTasks)
		return nil
	}

	prefixes, err := parseSelectorPrefixes(sc.Source.SrcCIDRs)
	if err != nil {
		return err
	}
	ports, err := parseSelectorPortRanges(sc.Source.SrcPortRanges)
	if err != nil {
		return err
	}

	hasIP := len(prefixes) > 0
	hasPort := len(ports) > 0
	for _, prefix := range prefixes {
		if hasPort {
			for _, pr := range ports {
				if prefix.Bits() == prefix.Addr().BitLen() && pr.Start == pr.End {
					addrPort := netip.AddrPortFrom(prefix.Addr(), pr.Start)
					state.ExactAddrPort[addrPort] = mergeTaskStateSliceUnique(state.ExactAddrPort[addrPort], selectorTasks)
					continue
				}
				bucket := CIDRPortDispatchBucket{Prefix: prefix, Start: pr.Start, End: pr.End, Tasks: selectorTasks}
				state.CIDRPortBuckets = mergeCIDRPortBucket(state.CIDRPortBuckets, bucket)
			}
			continue
		}
		if prefix.Bits() == prefix.Addr().BitLen() {
			state.ByIP[prefix.Addr()] = mergeTaskStateSliceUnique(state.ByIP[prefix.Addr()], selectorTasks)
			continue
		}
		bucket := CIDRDispatchBucket{Prefix: prefix, Tasks: selectorTasks}
		state.CIDRBuckets = mergeCIDRBucket(state.CIDRBuckets, bucket)
	}
	if hasIP {
		return nil
	}
	for _, pr := range ports {
		if !hasPort {
			continue
		}
		if pr.Start == pr.End {
			state.ByPort[pr.Start] = mergeTaskStateSliceUnique(state.ByPort[pr.Start], selectorTasks)
			continue
		}
		bucket := PortRangeDispatchBucket{Start: pr.Start, End: pr.End, Tasks: selectorTasks}
		state.PortBuckets = mergePortRangeBucket(state.PortBuckets, bucket)
	}
	return nil
}

type selectorPortRange struct {
	Start uint16
	End   uint16
}

func parseSelectorPrefixes(values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, err
			}
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()).Masked())
	}
	return out, nil
}

func parseSelectorPortRanges(values []string) ([]selectorPortRange, error) {
	out := make([]selectorPortRange, 0, len(values))
	for _, value := range values {
		if !strings.Contains(value, "-") {
			port, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return nil, err
			}
			out = append(out, selectorPortRange{Start: uint16(port), End: uint16(port)})
			continue
		}
		parts := strings.SplitN(value, "-", 2)
		start, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return nil, err
		}
		end, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return nil, err
		}
		out = append(out, selectorPortRange{Start: uint16(start), End: uint16(end)})
	}
	return out, nil
}

func mergeTaskStateSliceUnique(dst []*TaskState, src []*TaskState) []*TaskState {
	for _, ts := range src {
		if !containsTaskState(dst, ts) {
			dst = append(dst, ts)
		}
	}
	return dst
}

func containsTaskState(items []*TaskState, want *TaskState) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func mergeCIDRBucket(dst []CIDRDispatchBucket, add CIDRDispatchBucket) []CIDRDispatchBucket {
	for i := range dst {
		if dst[i].Prefix == add.Prefix {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRDispatchBucket{Prefix: add.Prefix, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

func mergePortRangeBucket(dst []PortRangeDispatchBucket, add PortRangeDispatchBucket) []PortRangeDispatchBucket {
	for i := range dst {
		if dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, PortRangeDispatchBucket{Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

func mergeCIDRPortBucket(dst []CIDRPortDispatchBucket, add CIDRPortDispatchBucket) []CIDRPortDispatchBucket {
	for i := range dst {
		if dst[i].Prefix == add.Prefix && dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRPortDispatchBucket{Prefix: add.Prefix, Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

func (s *Store) matchDispatchTasks(receiver string, pkt *packet.Packet) []*TaskState {
	v := s.dispatchSubs.Load()
	if v == nil {
		return nil
	}
	state := v.(map[string]*ReceiverSelectorDispatchState)[receiver]
	if state == nil {
		return nil
	}
	addrPort, ok := packetRemoteAddrPort(pkt)
	if !ok {
		return state.DefaultTasks
	}
	return state.match(addrPort)
}

func (state *ReceiverSelectorDispatchState) match(addrPort netip.AddrPort) []*TaskState {
	var matched []*TaskState
	var seen map[*TaskState]struct{}
	appendTasks := func(tasks []*TaskState) {
		if len(tasks) == 0 {
			return
		}
		for _, ts := range tasks {
			if len(matched) == 0 {
				matched = append(matched, ts)
				continue
			}
			if seen == nil {
				duplicate := false
				for _, existing := range matched {
					if existing == ts {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				if len(matched) == 1 {
					matched = append(matched, ts)
					continue
				}
				seen = make(map[*TaskState]struct{}, len(matched)+len(tasks))
				for _, existing := range matched {
					seen[existing] = struct{}{}
				}
			}
			if seen != nil {
				if _, exists := seen[ts]; exists {
					continue
				}
				seen[ts] = struct{}{}
			}
			matched = append(matched, ts)
		}
	}

	appendTasks(state.ExactAddrPort[addrPort])
	appendTasks(state.ByIP[addrPort.Addr()])
	appendTasks(state.ByPort[addrPort.Port()])
	for _, bucket := range state.CIDRBuckets {
		if bucket.Prefix.Contains(addrPort.Addr()) {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.PortBuckets {
		if addrPort.Port() >= bucket.Start && addrPort.Port() <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.CIDRPortBuckets {
		if bucket.Prefix.Contains(addrPort.Addr()) && addrPort.Port() >= bucket.Start && addrPort.Port() <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return state.DefaultTasks
}

func packetRemoteAddrPort(pkt *packet.Packet) (netip.AddrPort, bool) {
	if pkt == nil {
		return netip.AddrPort{}, false
	}
	remote := strings.TrimSpace(pkt.Meta.Remote)
	if remote == "" {
		return netip.AddrPort{}, false
	}
	if addrPort, err := netip.ParseAddrPort(remote); err == nil {
		return addrPort, true
	}
	host, portStr, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.AddrPort{}, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.AddrPort{}, false
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(addr, uint16(port)), true
}
