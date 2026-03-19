package task

import "context"

// testNamedSender 为 task 包测试提供复用的 sender 标识与空关闭实现。
//
// 这样每个测试桩只需实现自身的 Send 行为，避免在多个测试文件中重复维护相同的 Name/Key/Close 样板代码。
type testNamedSender struct {
	name string
}

// Name 返回测试 sender 名称。
func (s *testNamedSender) Name() string { return s.name }

// Key 返回测试 sender 唯一键；task 包测试按名称区分 sender 即可。
func (s *testNamedSender) Key() string { return s.name }

// Close 为纯内存测试场景提供空关闭实现。
func (s *testNamedSender) Close(context.Context) error { return nil }
