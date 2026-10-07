# 练习 17B：把回调和可信事实接进运行链

17A 类似前端的 `schema + some()`；17B 更像在应用根部做两个 Adapter：

```text
Codex event
  → EventSink callback
  → Store.AppendTaskEvents

running TaskRun
  → RequestBuilder
  → 服务端可信 ExecutionFacts
  → RunRequest
```

## 先理解闭包：它不是 Go 特有语法

闭包就是：**一个函数使用了它外层作用域里的变量，并且在外层函数返回后仍然记得这些变量。**

你在前端其实经常写闭包：

```ts
function createSaveHandler(api: API) {
  return async function save(value: string) {
    // save 使用了外层的 api，所以它是闭包。
    await api.save(value)
  }
}

const save = createSaveHandler(api)
await save("hello")
```

等价的 Go 形状：

```go
func newSaveHandler(api API) func(context.Context, string) error {
    return func(ctx context.Context, value string) error {
        // 返回的匿名函数记住了外层参数 api。
        return api.Save(ctx, value)
    }
}
```

这里有三个角色：

```text
newSaveHandler(api)       外层构造函数，只执行一次
api                       被捕获的依赖
func(ctx, value) error    返回的闭包，可以执行很多次
```

17B 的两个构造函数就是这个结构：

```text
NewStoreEventSink(appender)
  → 返回 EventSink 闭包
  → 闭包记住 appender

NewTrustedRequestBuilder(reader)
  → 返回 RequestBuilder 闭包
  → 闭包记住 reader
```

为什么不让每次调用都重新传 `appender`？因为它是应用启动时就确定的稳定依赖。真正每次变化的
只有 `ctx`、`event` 或 `TaskRun`。这和 React 组件外先创建 API client，再让点击处理函数持续
使用同一个 client 是同一个思路；闭包本身不等于 `useCallback`，`useCallback` 只是缓存闭包引用。

## 练习一：EventSink

前端组件可能只接收：

```ts
type OnChange = (value: Value) => Promise<void>
```

Go 的 `EventSink` 同样只是函数类型。`NewStoreEventSink` 返回闭包，把单条事件包装成切片，
再调用 Store 已有的批量接口。它不负责生成 sequence，也不负责 SSE。

## 练习二：RequestBuilder

`TaskRun` 只说明“执行哪张票、当前是什么状态”，并没有仓库路径和 Prompt。Builder 必须从
服务端可信来源补齐这些事实，不能把浏览器 body 直接转成 `RunRequest`。

前端可以类比成：页面只提交资源 ID，服务端再根据 ID 查询数据库中的真实 owner、权限和配置，
而不是相信页面同时提交的 `isAdmin: true`。

## 推荐实现顺序

1. `NewStoreEventSink` 的 nil 检查；
2. 返回闭包并把一条事件交给 `AppendTaskEvents`；
3. `NewTrustedRequestBuilder` 的 nil 检查；
4. 在闭包中检查 context 和 running TaskRun；
5. 读取 ExecutionFacts 并核对三个身份字段；
6. 构造、复制并校验 RunRequest；
7. 用测试确认 context 取消、身份错配、Store/Reader 错误都能沿调用链返回。

每完成一小步可运行：

```bash
go test ./apps/server/internal/runner
```

本练习不修改 `main.go`。17B 已完成；17C 才会在进程入口创建真实 Store、Builder、Sink、
Executor、Worker 和 Supervisor。
