// flags.go 定义 pipeline 在处理过程中使用的位标记。
package pipeline

const (
	FlagMatched   uint32 = 1 << 0
	FlagRewritten uint32 = 1 << 1
	FlagDrop      uint32 = 1 << 2
)
