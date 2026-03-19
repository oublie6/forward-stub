// Package runtime 负责维护转发运行时对象及其测试辅助逻辑。
package runtime

import "context"

// testNamedSender 提供 runtime 测试常见的 sender 标识与空 Close 实现。
//
// 设计目的：
//   - 避免多个测试桩重复实现完全相同的 Name/Key/Close 方法；
//   - 让每个测试发送端只关注自身的 Send 行为与断言状态；
//   - key 为空时默认回退为 name，满足多数测试只按名称区分 sender 的场景。
type testNamedSender struct {
	// name 是测试 sender 的展示名称。
	name string
	// key 是测试 sender 的唯一键；为空时复用 name。
	key string
}

// Name 返回测试 sender 名称。
func (s *testNamedSender) Name() string { return s.name }

// Key 返回测试 sender 唯一键；未显式设置时回退到 name。
func (s *testNamedSender) Key() string {
	if s.key != "" {
		return s.key
	}
	return s.name
}

// Close 为多数纯内存测试场景提供空关闭实现。
func (s *testNamedSender) Close(context.Context) error { return nil }
