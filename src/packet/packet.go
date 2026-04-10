// packet.go 定义统一报文结构与复制、释放等方法。
package packet

// Proto 表示报文来源协议类型。
// 用法：由 receiver 在入站时写入，供 pipeline/sender 判断来源与后续路由策略。
type Proto uint8

// PayloadKind 表示 payload 语义类型（流式/文件分块）。
// 用法：sender 根据 Kind 决定按“普通消息”还是“文件组装”处理 Payload。
type PayloadKind uint8

const (
	ProtoUDP    Proto = 1
	ProtoTCP    Proto = 2
	ProtoKafka  Proto = 3
	ProtoSFTP   Proto = 4
	ProtoSkyDDS Proto = 5
)

const (
	PayloadKindStream    PayloadKind = 1
	PayloadKindFileChunk PayloadKind = 2
)

// Meta 是统一包头元信息。
//
// 使用方式：
//  1. receiver 填充协议来源、地址、文件分块信息；
//  2. pipeline 可读取/修改元信息以实现路由与语义转换；
//  3. sender 按 Kind + Meta 决定输出协议与落盘行为。
type Meta struct {
	// Proto 表示该数据最初来源协议。
	// 用法：用于跨协议转发时保留溯源信息，便于审计与条件处理。
	Proto Proto
	// ReceiverName 是产出该 packet 的 receiver 配置名。
	// 用法：用于记录接入来源，便于 selector 路由后的排障与审计。
	ReceiverName string
	// Remote 是上游对端地址或远端资源标识。
	// 用法：网络协议通常写 socket 对端；文件协议可写远端路径。
	Remote string
	// Local 是本地接入地址或本地资源标识。
	// 用法：用于区分来自哪个入口实例，辅助排障。
	Local string
	// MatchKey 是 receiver 显式构造的唯一匹配字符串。
	// 用法：selector 仅使用该字段做完整字符串精确匹配，不再推断协议语义。
	MatchKey string

	// TransferID 是文件传输会话标识。
	// 用法：file_chunk 场景下用于 sender 聚合同一文件的多个分块。
	TransferID string
	// FileName 是文件名（不含目录）。
	// 用法：sender 可直接据此生成落盘文件名。
	FileName string
	// FilePath 是文件完整路径。
	// 用法：保留来源路径上下文，便于目录映射与回放定位。
	FilePath string
	// Offset 是当前 chunk 在文件中的起始偏移。
	// 用法：sender 通过 WriteAt(offset) 实现乱序/重传安全写入。
	Offset int64
	// TotalSize 是原始文件总大小（字节）。
	// 用法：与 EOF 一起判断是否可以提交最终文件。
	TotalSize int64
	// Checksum 是当前 payload 的摘要（通常 sha256）。
	// 用法：sender 可进行端到端分块校验，提前发现数据损坏。
	Checksum string
	// EOF 标记该 chunk 是否为该文件最后一个分块。
	// 用法：sender 通常在 EOF 且写满 total_size 后触发最终提交。
	EOF bool
	// RouteSender 是 pipeline 可选填充的“目标 sender 名称”。
	// 用法：当该值非空时，task 仅向同名 sender 发送，支持单任务内按字段分流。
	RouteSender string
}

// Envelope 是实际传递给 pipeline/sender 的数据单元。
// 用法：包含“语义头(Meta)+载荷(Payload)”；大多数处理逻辑都基于此结构。
type Envelope struct {
	// Kind 描述 Payload 的业务语义。
	// 用法：例如 PayloadKindFileChunk 表示需按文件分块流程处理。
	Kind PayloadKind
	// Meta 是与 Payload 对应的上下文信息。
	// 用法：在 pipeline 中可被读取或改写以影响下游行为。
	Meta Meta

	// Payload 是实际字节数据。
	// 用法：可为网络帧或文件分块内容，处理完成后应通过 Packet.Release 回收。
	Payload []byte
}

// Packet 是 Envelope 的可释放包装。
//
// 说明：
// - Payload 可能来自对象池；
// - ReleaseFn 用于归还内存，调用方处理完成后必须调用 Release。
type Packet struct {
	Envelope
	ReleaseFn func()
}

// Release 释放当前 packet 占用的可回收资源。
func (p *Packet) Release() {
	if p.ReleaseFn != nil {
		p.ReleaseFn()
		p.ReleaseFn = nil
	}
}

// Clone 深拷贝 payload 与 envelope，返回独立可释放副本。
// 常用于一个输入包需要广播到多个 task/sender 的场景。
// 用法：调用方拿到副本后也必须各自调用 Release，避免池内存泄漏。
func (p *Packet) Clone() *Packet {
	out, rel := CopyFrom(p.Payload)
	cp := p.Envelope
	cp.Payload = out
	return &Packet{Envelope: cp, ReleaseFn: rel}
}
