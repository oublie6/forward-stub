# Pipeline

pipeline 的职责没有变化：它仍然是 task 内部的处理链。

## 1. 当前关系

```text
receiver -> selector -> task -> pipeline -> sender
```

注意：

- pipeline 不参与 selector 匹配。
- pipeline 不负责选择 task。
- selector 命中完成后，task 才开始执行 pipeline。

## 2. 现有 stage

- `match_offset_bytes`
- `replace_offset_bytes`
- `mark_as_file_chunk`
- `clear_file_meta`
- `route_offset_bytes_sender`

其中 `route_offset_bytes_sender` 仍然只在 **task 内部** 决定 sender，不参与 receiver 到 task 的选择。
