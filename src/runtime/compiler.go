// Package runtime 负责把配置编译为运行时对象并维护热更新所需的缓存结构。
package runtime

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

// CompilePipelines 将配置中的 pipeline 定义编译为可直接执行的运行时对象。
//
// 该函数主要用于冷启动或独立测试场景，不依赖 Store 内的 stage 复用缓存；
// 若某个 stage 配置非法，会立即返回错误并终止整个编译过程。
func CompilePipelines(cfg map[string][]config.StageConfig) (map[string]*CompiledPipeline, error) {
	out := make(map[string]*CompiledPipeline, len(cfg))
	for name, stagesCfg := range cfg {
		pl := &pipeline.Pipeline{
			Name:   name,
			Stages: make([]pipeline.StageFunc, 0, len(stagesCfg)),
		}
		for _, sc := range stagesCfg {
			st, err := compileStage(sc)
			if err != nil {
				return nil, fmt.Errorf("pipeline %s stage %s error: %w", name, sc.Type, err)
			}
			pl.Stages = append(pl.Stages, st)
		}
		out[name] = &CompiledPipeline{Name: name, P: pl}
	}
	return out, nil
}

// stageSignature 为单个 stage 配置生成稳定签名，用于在热更新时识别可复用的 stage。
func stageSignature(sc config.StageConfig) (string, error) {
	b, err := json.Marshal(sc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// compilePipelinesWithStageCache 编译 pipeline，并尽量复用 Store 中已有的 stage 函数实例。
//
// 返回值中的第二个 map 记录每条 pipeline 的 stage 签名顺序，后续 task 引用计数、
// 缓存清理和全量纠偏都会依赖这份映射。
func (st *Store) compilePipelinesWithStageCache(cfg map[string][]config.StageConfig) (map[string]*CompiledPipeline, map[string][]string, error) {
	out := make(map[string]*CompiledPipeline, len(cfg))
	sigsByPipeline := make(map[string][]string, len(cfg))
	for name, stagesCfg := range cfg {
		pl := &pipeline.Pipeline{Name: name, Stages: make([]pipeline.StageFunc, 0, len(stagesCfg))}
		sigs := make([]string, 0, len(stagesCfg))
		for _, sc := range stagesCfg {
			sig, err := stageSignature(sc)
			if err != nil {
				return nil, nil, fmt.Errorf("pipeline %s stage %s signature error: %w", name, sc.Type, err)
			}

			st.mu.RLock()
			cached := st.stageCache[sig]
			st.mu.RUnlock()
			if cached != nil {
				pl.Stages = append(pl.Stages, cached.Fn)
				sigs = append(sigs, sig)
				continue
			}

			fn, err := compileStage(sc)
			if err != nil {
				return nil, nil, fmt.Errorf("pipeline %s stage %s error: %w", name, sc.Type, err)
			}
			st.mu.Lock()
			entry := st.stageCache[sig]
			if entry == nil {
				entry = &StageCacheEntry{Sig: sig, Fn: fn, Tasks: make(map[string]struct{})}
				st.stageCache[sig] = entry
			}
			st.mu.Unlock()

			pl.Stages = append(pl.Stages, entry.Fn)
			sigs = append(sigs, sig)
		}
		out[name] = &CompiledPipeline{Name: name, P: pl}
		sigsByPipeline[name] = sigs
	}
	return out, sigsByPipeline, nil
}

// compileStage 将单个 stage 配置转换为真正执行的函数。
//
// 该函数只负责“配置到函数”的编译，不处理 stage 复用；调用方需要自行决定是否缓存返回值。
func compileStage(sc config.StageConfig) (pipeline.StageFunc, error) {
	switch sc.Type {
	case "match_offset_bytes":
		b, err := hex.DecodeString(sc.Hex)
		if err != nil {
			return nil, err
		}
		return pipeline.MatchOffsetBytes(sc.Offset, b), nil
	case "replace_offset_bytes":
		b, err := hex.DecodeString(sc.Hex)
		if err != nil {
			return nil, err
		}
		return pipeline.ReplaceOffsetBytes(sc.Offset, b), nil
	case "mark_as_file_chunk":
		eof := true
		if sc.Bool != nil {
			eof = *sc.Bool
		}
		return pipeline.MarkAsFileChunk(sc.Path, eof), nil
	case "clear_file_meta":
		return pipeline.ClearFileMeta(), nil

	case "route_offset_bytes_sender":
		if len(sc.Cases) == 0 {
			return nil, fmt.Errorf("route_offset_bytes_sender requires non-empty cases")
		}
		keys := make([]string, 0, len(sc.Cases))
		for k := range sc.Cases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		routes := make(map[string]string, len(sc.Cases))
		keyLen := -1
		for _, hk := range keys {
			v, err := hex.DecodeString(hk)
			if err != nil {
				return nil, fmt.Errorf("route case %s invalid hex: %w", hk, err)
			}
			if len(v) == 0 {
				return nil, fmt.Errorf("route case %s empty hex", hk)
			}
			if keyLen < 0 {
				keyLen = len(v)
			} else if len(v) != keyLen {
				return nil, fmt.Errorf("route case %s length mismatch, want %d got %d", hk, keyLen, len(v))
			}
			sn := sc.Cases[hk]
			if sn == "" {
				return nil, fmt.Errorf("route case %s sender empty", hk)
			}
			routes[string(v)] = sn
		}
		return pipeline.RouteSenderByOffsetBytes(sc.Offset, keyLen, routes, sc.DefaultSender), nil
	default:
		return nil, fmt.Errorf("unknown stage type: %s", sc.Type)
	}
}
