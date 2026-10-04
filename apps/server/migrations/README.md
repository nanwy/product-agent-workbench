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
