package runtime

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"forward-stub/src/config"
	"forward-stub/src/packet"
)

// ReceiverSelectorDispatchState 是单个 receiver 对应的 selector dispatch 快照。
//
// 热路径说明：
//   - dispatch 先按 receiver 名称定位本结构；
//   - IPv4 精确规则使用整数 key 的 map 快路径（ip:port / ip / port）；
//   - 范围规则只保留轻量 bucket，避免在快照构建阶段暴力展开大范围 CIDR 或端口区间；
//   - default selector 只在全部 source selector miss 时兜底。
type ReceiverSelectorDispatchState struct {
	ExactAddrPort map[uint64][]*TaskState
	ByIP          map[uint32][]*TaskState
	ByPort        map[uint16][]*TaskState

	CIDRBuckets     []CIDRDispatchBucket
	PortBuckets     []PortRangeDispatchBucket
	CIDRPortBuckets []CIDRPortDispatchBucket

	DefaultTasks []*TaskState

	ExactAddrPort6 map[netip.AddrPort][]*TaskState
	ByIP6          map[netip.Addr][]*TaskState
	CIDRBuckets6   []CIDRDispatchBucket6
	CIDRPort6      []CIDRPortDispatchBucket6
}

// CIDRDispatchBucket 表示“仅按 IPv4 源网段”匹配的 selector bucket。
type CIDRDispatchBucket struct {
	Network uint32
	Mask    uint32
	Tasks   []*TaskState
}

// PortRangeDispatchBucket 表示“仅按源端口范围”匹配的 selector bucket。
type PortRangeDispatchBucket struct {
	Start uint16
	End   uint16
	Tasks []*TaskState
}

// CIDRPortDispatchBucket 表示“IPv4 源网段 + 源端口范围”联合匹配的 selector bucket。
type CIDRPortDispatchBucket struct {
	Network uint32
	Mask    uint32
	Start   uint16
	End     uint16
	Tasks   []*TaskState
}

// CIDRDispatchBucket6 保存非 IPv4 源网段规则，作为兼容性 fallback 使用。
type CIDRDispatchBucket6 struct {
	Prefix netip.Prefix
	Tasks  []*TaskState
}

// CIDRPortDispatchBucket6 保存非 IPv4 的网段+端口联合规则，作为兼容性 fallback 使用。
type CIDRPortDispatchBucket6 struct {
	Prefix netip.Prefix
	Start  uint16
	End    uint16
	Tasks  []*TaskState
}

// selectorPrefix 保存编译 selector source 时得到的已规范化前缀信息。
type selectorPrefix struct {
	Prefix  netip.Prefix
	IsIPv4  bool
	Network uint32
	Mask    uint32
}

// selectorPortRange 保存规范化后的源端口范围。
type selectorPortRange struct {
	Start uint16
	End   uint16
}

// selectorSource 保存从 packet 提取出的结构化源地址，用于热路径匹配。
type selectorSource struct {
	IPv4        uint32
	Port        uint16
	HasIPv4     bool
	GenericAddr netip.AddrPort
	HasGeneric  bool
}

// buildSelectorDispatchSnapshot 把 selector 配置编译为 receiver 维度的只读 dispatch 快照。
func buildSelectorDispatchSnapshot(selectors map[string]config.SelectorConfig, tasks map[string]*TaskState) (map[string]*ReceiverSelectorDispatchState, error) {
	snapshot := make(map[string]*ReceiverSelectorDispatchState)
	for selectorName, sc := range selectors {
		selectorTasks := selectorTasksForDispatch(sc, tasks)
		for _, receiverName := range sc.Receivers {
			state := snapshot[receiverName]
			if state == nil {
				state = &ReceiverSelectorDispatchState{
					ExactAddrPort:  make(map[uint64][]*TaskState),
					ByIP:           make(map[uint32][]*TaskState),
					ByPort:         make(map[uint16][]*TaskState),
					ExactAddrPort6: make(map[netip.AddrPort][]*TaskState),
					ByIP6:          make(map[netip.Addr][]*TaskState),
				}
				snapshot[receiverName] = state
			}
			if err := compileSelectorIntoDispatchState(state, selectorName, sc, selectorTasks); err != nil {
				return nil, fmt.Errorf("selector %s receiver %s compile error: %w", selectorName, receiverName, err)
			}
		}
	}
	return snapshot, nil
}

// selectorTasksForDispatch 把 selector 引用的 task 名称解析为运行时 TaskState 列表。
func selectorTasksForDispatch(sc config.SelectorConfig, tasks map[string]*TaskState) []*TaskState {
	out := make([]*TaskState, 0, len(sc.Tasks))
	for _, taskName := range sc.Tasks {
		if ts := tasks[taskName]; ts != nil {
			out = append(out, ts)
		}
	}
	return out
}

// compileSelectorIntoDispatchState 把单个 selector 编译进某个 receiver 的 dispatch state。
func compileSelectorIntoDispatchState(state *ReceiverSelectorDispatchState, _ string, sc config.SelectorConfig, selectorTasks []*TaskState) error {
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
				if prefix.IsIPv4 && prefix.Prefix.Bits() == 32 && pr.Start == pr.End {
					state.ExactAddrPort[composeAddrPortKey(prefix.Network, pr.Start)] = mergeTaskStateSliceUnique(state.ExactAddrPort[composeAddrPortKey(prefix.Network, pr.Start)], selectorTasks)
					continue
				}
				if prefix.IsIPv4 {
					state.CIDRPortBuckets = mergeCIDRPortBucket(state.CIDRPortBuckets, CIDRPortDispatchBucket{Network: prefix.Network, Mask: prefix.Mask, Start: pr.Start, End: pr.End, Tasks: selectorTasks})
					continue
				}
				state.CIDRPort6 = mergeCIDRPortBucket6(state.CIDRPort6, CIDRPortDispatchBucket6{Prefix: prefix.Prefix, Start: pr.Start, End: pr.End, Tasks: selectorTasks})
			}
			continue
		}
		if prefix.IsIPv4 {
			if prefix.Prefix.Bits() == 32 {
				state.ByIP[prefix.Network] = mergeTaskStateSliceUnique(state.ByIP[prefix.Network], selectorTasks)
				continue
			}
			state.CIDRBuckets = mergeCIDRBucket(state.CIDRBuckets, CIDRDispatchBucket{Network: prefix.Network, Mask: prefix.Mask, Tasks: selectorTasks})
			continue
		}
		if prefix.Prefix.Bits() == prefix.Prefix.Addr().BitLen() {
			state.ByIP6[prefix.Prefix.Addr()] = mergeTaskStateSliceUnique(state.ByIP6[prefix.Prefix.Addr()], selectorTasks)
			continue
		}
		state.CIDRBuckets6 = mergeCIDRBucket6(state.CIDRBuckets6, CIDRDispatchBucket6{Prefix: prefix.Prefix, Tasks: selectorTasks})
	}
	if hasIP {
		return nil
	}
	for _, pr := range ports {
		if pr.Start == pr.End {
			state.ByPort[pr.Start] = mergeTaskStateSliceUnique(state.ByPort[pr.Start], selectorTasks)
			continue
		}
		state.PortBuckets = mergePortRangeBucket(state.PortBuckets, PortRangeDispatchBucket{Start: pr.Start, End: pr.End, Tasks: selectorTasks})
	}
	return nil
}

// parseSelectorPrefixes 把 selector 的 CIDR/IP 配置解析为已 masked 的前缀列表。
func parseSelectorPrefixes(values []string) ([]selectorPrefix, error) {
	out := make([]selectorPrefix, 0, len(values))
	for _, value := range values {
		var prefix netip.Prefix
		var err error
		if strings.Contains(value, "/") {
			prefix, err = netip.ParsePrefix(value)
			if err != nil {
				return nil, err
			}
			prefix = prefix.Masked()
		} else {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return nil, err
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen()).Masked()
		}
		compiled := selectorPrefix{Prefix: prefix, IsIPv4: prefix.Addr().Is4()}
		if compiled.IsIPv4 {
			compiled.Network = ipv4ToUint32(prefix.Addr())
			compiled.Mask = prefixMask(prefix.Bits())
			compiled.Network &= compiled.Mask
		}
		out = append(out, compiled)
	}
	return out, nil
}

// parseSelectorPortRanges 把 selector 的端口/端口范围配置解析为结构化范围。
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
		if dst[i].Network == add.Network && dst[i].Mask == add.Mask {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRDispatchBucket{Network: add.Network, Mask: add.Mask, Tasks: append([]*TaskState(nil), add.Tasks...)})
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
		if dst[i].Network == add.Network && dst[i].Mask == add.Mask && dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRPortDispatchBucket{Network: add.Network, Mask: add.Mask, Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

func mergeCIDRBucket6(dst []CIDRDispatchBucket6, add CIDRDispatchBucket6) []CIDRDispatchBucket6 {
	for i := range dst {
		if dst[i].Prefix == add.Prefix {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRDispatchBucket6{Prefix: add.Prefix, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

func mergeCIDRPortBucket6(dst []CIDRPortDispatchBucket6, add CIDRPortDispatchBucket6) []CIDRPortDispatchBucket6 {
	for i := range dst {
		if dst[i].Prefix == add.Prefix && dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRPortDispatchBucket6{Prefix: add.Prefix, Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

// matchDispatchTasks 根据 receiver 名称和 packet 元信息返回命中的 task 集。
func (s *Store) matchDispatchTasks(receiver string, pkt *packet.Packet) []*TaskState {
	v := s.dispatchSubs.Load()
	if v == nil {
		return nil
	}
	state := v.(map[string]*ReceiverSelectorDispatchState)[receiver]
	if state == nil {
		return nil
	}
	source := packetSelectorSource(pkt)
	if source.HasIPv4 {
		return state.matchIPv4(source.IPv4, source.Port)
	}
	if source.HasGeneric {
		return state.matchGeneric(source.GenericAddr)
	}
	return state.DefaultTasks
}

func (state *ReceiverSelectorDispatchState) matchIPv4(ip uint32, port uint16) []*TaskState {
	var matched []*TaskState
	var seen map[*TaskState]struct{}
	appendTasks := func(tasks []*TaskState) {
		matched, seen = appendUniqueTaskStates(matched, seen, tasks)
	}

	appendTasks(state.ExactAddrPort[composeAddrPortKey(ip, port)])
	appendTasks(state.ByIP[ip])
	appendTasks(state.ByPort[port])
	for _, bucket := range state.CIDRBuckets {
		if ip&bucket.Mask == bucket.Network {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.PortBuckets {
		if port >= bucket.Start && port <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.CIDRPortBuckets {
		if ip&bucket.Mask == bucket.Network && port >= bucket.Start && port <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return state.DefaultTasks
}

func (state *ReceiverSelectorDispatchState) matchGeneric(addrPort netip.AddrPort) []*TaskState {
	var matched []*TaskState
	var seen map[*TaskState]struct{}
	appendTasks := func(tasks []*TaskState) {
		matched, seen = appendUniqueTaskStates(matched, seen, tasks)
	}

	appendTasks(state.ExactAddrPort6[addrPort])
	appendTasks(state.ByIP6[addrPort.Addr()])
	appendTasks(state.ByPort[addrPort.Port()])
	for _, bucket := range state.CIDRBuckets6 {
		if bucket.Prefix.Contains(addrPort.Addr()) {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.PortBuckets {
		if addrPort.Port() >= bucket.Start && addrPort.Port() <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	for _, bucket := range state.CIDRPort6 {
		if bucket.Prefix.Contains(addrPort.Addr()) && addrPort.Port() >= bucket.Start && addrPort.Port() <= bucket.End {
			appendTasks(bucket.Tasks)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return state.DefaultTasks
}

func appendUniqueTaskStates(matched []*TaskState, seen map[*TaskState]struct{}, tasks []*TaskState) ([]*TaskState, map[*TaskState]struct{}) {
	if len(tasks) == 0 {
		return matched, seen
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
		if _, exists := seen[ts]; exists {
			continue
		}
		seen[ts] = struct{}{}
		matched = append(matched, ts)
	}
	return matched, seen
}

func packetSelectorSource(pkt *packet.Packet) selectorSource {
	if pkt == nil {
		return selectorSource{}
	}
	if pkt.Meta.HasSrcAddr {
		return selectorSource{IPv4: pkt.Meta.SrcIPv4, Port: pkt.Meta.SrcPort, HasIPv4: true}
	}
	addrPort, ok := packet.ParseAddrPort(pkt.Meta.Remote)
	if !ok {
		return selectorSource{}
	}
	source := selectorSource{GenericAddr: addrPort, HasGeneric: true}
	if addrPort.Addr().Is4() {
		source.IPv4 = ipv4ToUint32(addrPort.Addr())
		source.Port = addrPort.Port()
		source.HasIPv4 = true
	}
	return source
}

func composeAddrPortKey(ip uint32, port uint16) uint64 {
	return (uint64(ip) << 16) | uint64(port)
}

func prefixMask(bits int) uint32 {
	if bits <= 0 {
		return 0
	}
	if bits >= 32 {
		return ^uint32(0)
	}
	return ^uint32(0) << (32 - bits)
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	v := addr.As4()
	return binary.BigEndian.Uint32(v[:])
}
