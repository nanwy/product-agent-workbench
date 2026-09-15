(() => {
  const steps = [
    {
      caption: "T0：服务端先创建空的 assistant 占位消息。",
      upstream: "尚未输出",
      server: "pending = ∅\nRequirement = ''",
      sse: "尚无批次",
      browser: "assistant(streaming) = ''",
    },
    {
      caption: "T1：app-server 给出第 1 个原始 delta。序号已经占用，但 50ms 尚未到。",
      upstream: "seq 1 / item-7 / '建'",
      server: "pendingText = '建'\npendingBytes = 3",
      sse: "batch[1..1] = '建'（待发）",
      browser: "仍是 ''",
    },
    {
      caption: "T2：同一个 item 又来一个 delta，后端把它并入同一批。",
      upstream: "seq 2 / item-7 / '议'",
      server: "pendingText = '建议'\npendingBytes = 6",
      sse: "batch[1..2] = '建议'（待发）",
      browser: "仍是 ''",
    },
    {
      caption: "T3：50ms 到期。SSE 只发一个批次，React Query 一次追加“建议”。",
      upstream: "暂时安静",
      server: "pendingText = '建议'\nRequirement 仍可暂未落盘",
      sse: "发送 batch[1..2] = '建议'",
      browser: "lastSeq = 2\n正文 = '建议'",
    },
    {
      caption: "T4：下一批原始 delta 到达。页面先快起来，持久化仍按自己的节奏走。",
      upstream: "seq 3 / '只修改'\nseq 4 / '登录页'",
      server: "pendingText = '建议只修改登录页'",
      sse: "batch[3..4] = '只修改登录页'（待发）",
      browser: "lastSeq = 2\n正文 = '建议'",
    },
    {
      caption: "T5：第二个 SSE 批次到达。页面已经看到完整句子。",
      upstream: "等待 item/completed",
      server: "累计全文 = '建议只修改登录页'",
      sse: "发送 batch[3..4]",
      browser: "lastSeq = 4\n正文 = '建议只修改登录页'",
    },
    {
      caption: "T6：500ms 持久化阈值触发。服务端保存累计全文，刷新后也能恢复。",
      upstream: "Turn 仍在运行",
      server: "Requirement = '建议只修改登录页'",
      sse: "无需为落盘伪造文字",
      browser: "正文不变",
    },
    {
      caption: "T7：item/completed 给出权威全文。先清空缓冲，再保存全文，最后发 invalidate。",
      upstream: "item.completed.text =\n'建议只修改登录页'",
      server: "用同 itemId 替换为权威全文",
      sse: "invalidate",
      browser: "重新取快照；内容相同，不重复追加",
    },
    {
      caption: "T8：turn/completed 收口。消息从 streaming 变成 completed，业务门禁才允许前进。",
      upstream: "turn.completed",
      server: "message.status = completed",
      sse: "invalidate",
      browser: "移除生成光标；采用终态快照",
    },
  ]

  let index = 0
  const byId = (id) => document.getElementById(id)
  const render = () => {
    const step = steps[index]
    byId("trace-caption").textContent = step.caption
    byId("trace-upstream").textContent = step.upstream
    byId("trace-server").textContent = step.server
    byId("trace-sse").textContent = step.sse
    byId("trace-browser").textContent = step.browser
    byId("trace-position").textContent = `${index + 1} / ${steps.length}`
  }

  byId("trace-next")?.addEventListener("click", () => {
    index = Math.min(index + 1, steps.length - 1)
    render()
  })
  byId("trace-prev")?.addEventListener("click", () => {
    index = Math.max(index - 1, 0)
    render()
  })
  byId("trace-reset")?.addEventListener("click", () => {
    index = 0
    render()
  })

  render()
})()
