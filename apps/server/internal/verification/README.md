# 练习 17A：从前端 `some` 到 Go 验证门禁

这一小步只写纯规则，不运行验证目标里的 `pnpm`、浏览器或 Git 命令；这里列出的
`go test` 只用于检查这段 Go 规则自身。

## 先用前端视角理解

页面现在大致这样判断是否受阻：

```ts
const hasBlockingRecord = records.some(
  (item) => item.required && item.result !== "verified",
)
```

Go 的 `Matrix.BlocksDelivery` 表达同一条业务规则。差别只是 Go 标准库没有数组的
`.some()` 链式方法，需要用 `for range` 和 early return。

```text
遍历 Records
  如果 Required 且 Status 不是 verified
    立即返回 true
遍历结束仍没找到
  返回 false
```

## 为什么还需要 BuildMatrix

前端收到不合法接口数据时，最多是页面渲染异常；后端如果把不合法证据持久化，重启后仍会
继续影响交付判断。因此进入 Matrix 前必须检查：

1. `ID`、`Label` 不是空白；
2. `Status` 只能是三个已知值；
3. `not-run` 必须解释为什么没运行；
4. 同一个 Matrix 不能出现重复 ID；
5. 返回新的 `Records` 切片，不持有调用方的切片。

## TypeScript 与 Go 语法对照

| TypeScript | Go |
| --- | --- |
| `const records: Evidence[] = []` | `records := make([]Evidence, 0, len(evidence))` |
| `const seen = new Set<string>()` | `seen := make(map[string]struct{}, len(evidence))` |
| `for (const item of evidence)` | `for _, item := range evidence` |
| `records.push(item)` | `records = append(records, item)` |
| `seen.has(item.id)` | `_, exists := seen[item.ID]` |
| `seen.add(item.id)` | `seen[item.ID] = struct{}{}` |
| `value.trim()` | `strings.TrimSpace(value)` |
| `throw new Error(...)` | `return Matrix{}, fmt.Errorf(...)` |

`struct{}{}` 是零字节的空结构体。这里不保存额外值，只利用 map 的 key 表示 ID 是否出现过。

## 推荐实现顺序

每完成一步先运行：

```bash
go test ./apps/server/internal/verification
```

1. 实现 `validateEvidence` 的 ID、Label 必填检查；
2. 增加 `switch`，拒绝未知 Status；
3. 增加 `not-run` 的 Reason 检查；
4. 实现 `BuildMatrix` 的 records、seenIDs 和循环；
5. 最后实现 `BlocksDelivery`。

## 自查场景

写完以后，先不要看答案，逐个在脑中运行：

| 输入 | 预期 |
| --- | --- |
| 空数组 | 合法空 Matrix，不阻止交付 |
| required + verified | 不阻止交付 |
| required + failed | 阻止交付 |
| required + not-run + Reason | 阻止交付 |
| not required + failed | 不阻止交付 |
| not-run 但 Reason 为空 | 返回 `ErrInvalidEvidence` |
| 两条记录 ID 相同 | 返回 `ErrInvalidEvidence` |
| Status 是 `"success"` | 返回 `ErrInvalidEvidence` |

17A 完成后再增加证据收集 Adapter。纯函数不应该知道命令、HTTP、Git 或浏览器是如何运行的。
