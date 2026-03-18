// stages_file.go 提供文件语义相关 stage，用于 stream/file_chunk 之间的转换。
package pipeline

import (
	"path"
	"strings"
	"time"

	"forward-stub/src/packet"
)

// MarkAsFileChunk 将实时数据标记为“文件分块语义”，用于 stream->file 转换。
func MarkAsFileChunk(defaultPath string, eof bool) StageFunc {
	return func(p *packet.Packet) bool {
		filePath := strings.TrimSpace(p.Meta.FilePath)
		if filePath == "" {
			filePath = strings.TrimSpace(defaultPath)
		}
		if filePath == "" {
			filePath = path.Join("stream", time.Now().Format("20060102-150405.000")+".bin")
		}
		if p.Meta.TransferID == "" {
			p.Meta.TransferID = filePath + "|" + time.Now().Format("150405.000000")
		}
		p.Meta.FilePath = filePath
		if p.Meta.TotalSize <= 0 {
			p.Meta.TotalSize = int64(len(p.Payload))
		}
		p.Kind = packet.PayloadKindFileChunk
		p.Meta.Offset = 0
		p.Meta.EOF = eof
		return true
	}
}

// ClearFileMeta 清理文件元信息，实现 file_chunk->stream 语义转换。
func ClearFileMeta() StageFunc {
	return func(p *packet.Packet) bool {
		p.Meta.FilePath = ""
		p.Meta.TransferID = ""
		p.Meta.Offset = 0
		p.Meta.TotalSize = 0
		p.Meta.EOF = false
		p.Kind = packet.PayloadKindStream
		return true
	}
}
