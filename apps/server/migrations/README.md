# SQLite Migration

这里将保存经过评审、只向前执行的 SQLite migration。

在第一阶段固定领域状态迁移前，不要创建第一份 SQL。写 SQL 前必须明确回答：

1. 哪些查询会列出 Requirement，按哪些列排序或过滤？
2. 哪条唯一约束阻止同一 Requirement 同时存在两个活动 TaskRun？
3. 哪些外键应该级联、限制删除或保留审计历史？
4. 调用方看到的 Requirement revision 如何参与每次写操作？
5. 事件 id 如何分配，SSE replay 保留多大的窗口？
6. 哪些 payload 字段必须在持久化前脱敏？
7. 每个索引服务于哪条明确查询，而不是凭感觉添加？

回答被记录后，再手写 migration，单独评审，并使用临时数据库和升级 fixture 验证。

## 当前学习入口

`internal/store/sqlite` 已提供 Adapter 方法骨架，但没有生成 migration 或锁定 driver。

实现顺序：

1. 先补一份设计记录，回答上面的七个问题，并固定 UTC 时间编码和稳定入队顺序。
2. 选择 SQLite driver，main 打开 `*sql.DB`，Adapter 只接收连接，不依赖具体 driver。
3. 人工编写第一份只向前 migration，并用临时数据库验证空库升级。
4. 把 Memory Store 契约提取为 factory，SQLite 必须通过同一套测试。
5. 依次实现 Requirement、TaskRun、原子 claim、TaskEvent；一次只解锁一组契约。
6. 用两个独立数据库连接运行 claim/revision 并发测试，而不只依赖 `go test -race`。

对应旧 TypeScript：`requirement-store.ts` 的 JSON 快照和 Promise 写队列会被事务与约束替代，
但 Spec/Ticket Markdown 仍是文件 artifact，不迁入这些表。切流前禁止同时写 JSON 和 SQLite。
