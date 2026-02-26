// compiler.go 负责把配置编译为 receiver/pipeline/task/sender 运行对象。
package runtime

import (
	"encoding/hex"
	"fmt"

	"forward-stub/src/config"
	"forward-stub/src/pipeline"
)

// CompilePipelines 负责该函数对应的核心逻辑，详见实现细节。
func CompilePipelines(cfg map[string][]config.StageConfig) (map[string]*CompiledPipeline, error) {
	out := make(map[string]*CompiledPipeline, len(cfg))
	for name, stagesCfg := range cfg {
		pl := &pipeline.Pipeline{Name: name}
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
