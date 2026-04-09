package pipeline

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"forward-stub/src/logx"
	"forward-stub/src/packet"
)

// SplitFileChunkToPackets 将 file_chunk payload 按固定 packetSize 拆成实时数据包序列。
func SplitFileChunkToPackets(packetSize int, preserveFileMeta bool) StageFunc {
	if packetSize <= 0 {
		return func([]*packet.Packet) []*packet.Packet { return nil }
	}
	return func(in []*packet.Packet) []*packet.Packet {
		out := make([]*packet.Packet, 0, len(in))
		for _, p := range in {
			if p.Kind != packet.PayloadKindFileChunk || len(p.Payload) == 0 {
				out = append(out, p)
				continue
			}
			for off := 0; off < len(p.Payload); off += packetSize {
				end := off + packetSize
				if end > len(p.Payload) {
					end = len(p.Payload)
				}
				chunk, rel := packet.CopyFrom(p.Payload[off:end])
				meta := p.Meta
				meta.Offset = p.Meta.Offset + int64(off)
				meta.EOF = p.Meta.EOF && end == len(p.Payload)
				if !preserveFileMeta {
					meta.TransferID = ""
					meta.FileName = ""
					meta.FilePath = ""
					meta.TotalSize = 0
					meta.Checksum = ""
				}
				out = append(out, &packet.Packet{
					Envelope: packet.Envelope{
						Kind:    packet.PayloadKindStream,
						Meta:    meta,
						Payload: chunk,
					},
					ReleaseFn: rel,
				})
			}
		}
		return out
	}
}

type streamSegmentState struct {
	seq         uint64
	currentID   string
	currentPath string
	segmentMeta packet.Meta
	segmentBuf  []byte
}

func buildBufferKey(receiverName, matchKey string) string {
	return receiverName + "|" + matchKey
}

// StreamPacketsToFileSegments 将实时数据包按滚动分段语义组装为 file_chunk 包。
func StreamPacketsToFileSegments(segmentSize int, chunkSize int, dir string, filePrefix string, timeLayout string) StageFunc {
	if segmentSize <= 0 || chunkSize <= 0 {
		return func([]*packet.Packet) []*packet.Packet { return nil }
	}
	layout := strings.TrimSpace(timeLayout)
	if layout == "" {
		layout = "20060102-150405"
	}
	prefix := strings.TrimSpace(filePrefix)
	if prefix == "" {
		prefix = "stream"
	}
	baseDir := strings.TrimSpace(dir)
	if baseDir == "" {
		baseDir = "stream"
	}
	states := make(map[string]*streamSegmentState)
	var mu sync.Mutex

	newSegment := func(st *streamSegmentState, now time.Time) {
		st.seq++
		name := fmt.Sprintf("%s_%s_%06d.bin", prefix, now.Format(layout), st.seq)
		st.currentPath = path.Join(baseDir, name)
		st.currentID = fmt.Sprintf("%s|%d", st.currentPath, now.UnixNano())
		st.segmentBuf = nil
	}

	buildChunk := func(st *streamSegmentState, meta packet.Meta, b []byte, eof bool, total int64, offset int64) *packet.Packet {
		payload, rel := packet.CopyFrom(b)
		meta.TransferID = st.currentID
		meta.FileName = path.Base(st.currentPath)
		meta.FilePath = st.currentPath
		meta.Offset = offset
		meta.TotalSize = total
		meta.EOF = eof
		return &packet.Packet{
			Envelope: packet.Envelope{
				Kind:    packet.PayloadKindFileChunk,
				Meta:    meta,
				Payload: payload,
			},
			ReleaseFn: rel,
		}
	}

	return func(in []*packet.Packet) []*packet.Packet {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*packet.Packet, 0)
		for _, p := range in {
			if len(p.Payload) == 0 {
				continue
			}
			receiverName := strings.TrimSpace(p.Meta.ReceiverName)
			if receiverName == "" {
				logx.L().Errorw("stream_packets_to_file_segments 拒绝进入缓冲：receiver_name 为空",
					"match_key", p.Meta.MatchKey,
					"kind", p.Kind,
				)
				continue
			}
			matchKey := strings.TrimSpace(p.Meta.MatchKey)
			if matchKey == "" {
				logx.L().Errorw("stream_packets_to_file_segments 拒绝进入缓冲：match_key 为空",
					"receiver_name", receiverName,
					"kind", p.Kind,
				)
				continue
			}
			bufferKey := buildBufferKey(receiverName, matchKey)
			st := states[bufferKey]
			if st == nil {
				st = &streamSegmentState{}
				states[bufferKey] = st
			}
			if st.currentID == "" {
				newSegment(st, time.Now())
				st.segmentMeta = p.Meta
			}
			buf := p.Payload
			for len(buf) > 0 {
				room := segmentSize - len(st.segmentBuf)
				if room <= 0 {
					newSegment(st, time.Now())
					st.segmentMeta = p.Meta
					room = segmentSize
				}
				take := len(buf)
				if take > room {
					take = room
				}
				st.segmentBuf = append(st.segmentBuf, buf[:take]...)
				buf = buf[take:]

				segmentReady := len(st.segmentBuf) == segmentSize
				if !segmentReady {
					continue
				}
				offset := int64(0)
				for off := 0; off < len(st.segmentBuf); off += chunkSize {
					n := chunkSize
					if off+n > len(st.segmentBuf) {
						n = len(st.segmentBuf) - off
					}
					eof := off+n == len(st.segmentBuf)
					out = append(out, buildChunk(st, st.segmentMeta, st.segmentBuf[off:off+n], eof, int64(len(st.segmentBuf)), offset))
					offset += int64(n)
				}

				if segmentReady {
					st.currentID = ""
					st.currentPath = ""
					st.segmentBuf = nil
					st.segmentMeta = packet.Meta{}
					if len(buf) > 0 {
						newSegment(st, time.Now())
						st.segmentMeta = p.Meta
					}
				}
			}
		}
		return out
	}
}
