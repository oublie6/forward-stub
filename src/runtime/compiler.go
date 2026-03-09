// compiler.go 负责把配置编译为 receiver/pipeline/task/sender 运行对象。
package runtime

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

// CompilePipelines 负责该函数对应的核心逻辑，详见实现细节。
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

const stageCacheGCInterval = 30 * time.Second

func stageSignature(sc config.StageConfig) (string, error) {
	b, err := json.Marshal(sc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

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
				entry = &StageCacheEntry{Sig: sig, Fn: fn, Tasks: make(map[string]struct{}), ZeroAt: time.Now()}
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

// compileStage 负责该函数对应的核心逻辑，详见实现细节。
func compileStage(sc config.StageConfig) (pipeline.StageFunc, error) {
	switch sc.Type {
	case "match_offset_bytes":
		b, err := hex.DecodeString(sc.Hex)
		if err != nil {
			return nil, err
		}
		return pipeline.MatchOffsetBytes(sc.Offset, b, flagFromName(sc.Flag)), nil
	case "replace_offset_bytes":
		b, err := hex.DecodeString(sc.Hex)
		if err != nil {
			return nil, err
		}
		return pipeline.ReplaceOffsetBytes(sc.Offset, b, flagFromName(sc.Flag)), nil
	case "drop_if_flag":
		return pipeline.DropIfFlag(flagFromName(sc.Flag)), nil
	case "mark_as_file_chunk":
		eof := true
		if sc.Bool != nil {
			eof = *sc.Bool
		}
		return pipeline.MarkAsFileChunk(sc.Path, eof), nil
	case "clear_file_meta":
		return pipeline.ClearFileMeta(), nil
	default:
		return nil, fmt.Errorf("unknown stage type: %s", sc.Type)
	}
}

// flagFromName 负责该函数对应的核心逻辑，详见实现细节。
func flagFromName(s string) uint32 {
	switch s {
	case "", "none":
		return 0
	case "matched":
		return pipeline.FlagMatched
	case "rewritten":
		return pipeline.FlagRewritten
	case "drop":
		return pipeline.FlagDrop
	default:
		return 0
	}
}
