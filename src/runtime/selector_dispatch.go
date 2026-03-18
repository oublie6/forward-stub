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

// ReceiverSelectorDispatchState 是单个 receiver 对应的 selector dispatch 快照。
//
// 热路径说明：
//   - dispatch 先按 receiver 名称定位本结构；
//   - 再按 packet.Meta.Remote 解析出的源地址/端口走精确匹配、范围匹配和 default fallback；
//   - 该结构只读、可被多个 goroutine 并发访问，因此适合通过 atomic.Value 整体替换。
type ReceiverSelectorDispatchState struct {
	ExactAddrPort   map[netip.AddrPort][]*TaskState
	ByIP            map[netip.Addr][]*TaskState
	ByPort          map[uint16][]*TaskState
	CIDRBuckets     []CIDRDispatchBucket
	PortBuckets     []PortRangeDispatchBucket
	CIDRPortBuckets []CIDRPortDispatchBucket
	DefaultTasks    []*TaskState
}

// CIDRDispatchBucket 表示“仅按源 IP CIDR”匹配的 selector bucket。
type CIDRDispatchBucket struct {
	Prefix netip.Prefix
	Tasks  []*TaskState
}

// PortRangeDispatchBucket 表示“仅按源端口范围”匹配的 selector bucket。
type PortRangeDispatchBucket struct {
	Start uint16
	End   uint16
	Tasks []*TaskState
}

// CIDRPortDispatchBucket 表示“源 IP CIDR + 源端口范围”联合匹配的 selector bucket。
type CIDRPortDispatchBucket struct {
	Prefix netip.Prefix
	Start  uint16
	End    uint16
	Tasks  []*TaskState
}

// buildSelectorDispatchSnapshot 把 selector 配置编译为 receiver 维度的只读 dispatch 快照。
//
// 返回值以 receiver 为第一层索引，保证 dispatch 热路径不会回退到“遍历所有 task”。
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

// selectorTasksForDispatch 把 selector 引用的 task 名称解析为运行时 TaskState 列表。
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

// compileSelectorIntoDispatchState 把单个 selector 编译进某个 receiver 的 dispatch state。
//
// 编译策略优先为 exact ip:port / ip / port 建立 map 快路径，只有无法精确索引的规则才保留为范围 bucket。
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

// selectorPortRange 保存规范化后的源端口范围。
type selectorPortRange struct {
	Start uint16
	End   uint16
}

// parseSelectorPrefixes 把 selector 的 CIDR/IP 配置解析为已 masked 的 netip.Prefix 列表。
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

// mergeTaskStateSliceUnique 合并 task 列表并去重，避免同一 task 因多个 selector bucket 命中而重复投递。
func mergeTaskStateSliceUnique(dst []*TaskState, src []*TaskState) []*TaskState {
	for _, ts := range src {
		if !containsTaskState(dst, ts) {
			dst = append(dst, ts)
		}
	}
	return dst
}

// containsTaskState is a package-local helper used by selector_dispatch.go.
func containsTaskState(items []*TaskState, want *TaskState) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// mergeCIDRBucket is a package-local helper used by selector_dispatch.go.
func mergeCIDRBucket(dst []CIDRDispatchBucket, add CIDRDispatchBucket) []CIDRDispatchBucket {
	for i := range dst {
		if dst[i].Prefix == add.Prefix {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRDispatchBucket{Prefix: add.Prefix, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

// mergePortRangeBucket is a package-local helper used by selector_dispatch.go.
func mergePortRangeBucket(dst []PortRangeDispatchBucket, add PortRangeDispatchBucket) []PortRangeDispatchBucket {
	for i := range dst {
		if dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, PortRangeDispatchBucket{Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

// mergeCIDRPortBucket is a package-local helper used by selector_dispatch.go.
func mergeCIDRPortBucket(dst []CIDRPortDispatchBucket, add CIDRPortDispatchBucket) []CIDRPortDispatchBucket {
	for i := range dst {
		if dst[i].Prefix == add.Prefix && dst[i].Start == add.Start && dst[i].End == add.End {
			dst[i].Tasks = mergeTaskStateSliceUnique(dst[i].Tasks, add.Tasks)
			return dst
		}
	}
	return append(dst, CIDRPortDispatchBucket{Prefix: add.Prefix, Start: add.Start, End: add.End, Tasks: append([]*TaskState(nil), add.Tasks...)})
}

// matchDispatchTasks 根据 receiver 名称和 packet 元信息返回命中的 task 集。
//
// 热路径说明：
//   - 只读取 atomic 快照，不持有 Store 主锁；
//   - 仅解析 packet.Meta.Remote，不分配额外中间结构；
//   - Remote 解析失败时不会 panic，而是安全回退到 default selector。
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

// match 在单个 receiver 的 selector 快照内完成 source 匹配。
//
// 优先级说明：
//   - 任意 source selector 命中时，仅返回 source selector 关联的 task；
//   - 只有所有 source selector 全 miss 时，才回退到 DefaultTasks。
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

// packetRemoteAddrPort 从 packet.Meta.Remote 提取源地址和端口。
//
// 输入通常来自 receiver 写入的 socket 对端地址；解析失败时返回 false，调用方应按“仅 default selector”处理。
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
