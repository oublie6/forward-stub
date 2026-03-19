# 运行时与生命周期

## 1. 启动阶段

`UpdateCache` 的核心步骤：

1. 编译 pipelines。
2. 构建 senders。
3. 构建 tasks。
4. 编译 selectors，把 `task_sets` 直接展开成 `[]*TaskState`。
5. 构建并启动 receivers。

## 2. dispatch 热路径

热路径固定为：

```text
packet.MatchKey -> selector map lookup -> []*TaskState -> fanout
```

特点：

- 不遍历规则。
- 不做表达式求值。
- 不做反射。
- 不做协议二次解析。

## 3. 热更新

热更新时会：

- 更新 receiver / selector / task_set / sender / pipeline / task 的快照。
- 在编译期重建 selector 结果。
- 把 task set 重新展开到新的 task slice。
- 保证 receiver 入口和 selector 快照保持一致。

## 4. 为什么 task set 不进入热路径

`task_set` 只是配置层概念。运行时会直接把：

```text
match key -> task_set_name
```

展开成：

```text
match key -> []*TaskState
```

这样可以避免热路径上的额外间接层。
