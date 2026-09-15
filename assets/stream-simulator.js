(() => {
  const snapshots = ["我建议", "我建议先确认", "我建议先确认目标页面。"];
  let step = 0;
  let naive = "";
  let safe = "";

  const byId = (id) => document.getElementById(id);

  function render() {
    byId("upstream").textContent = step ? `item-42 = ${snapshots[step - 1]}` : "等待事件…";
    byId("naive").textContent = naive || "等待事件…";
    byId("safe").textContent = safe || "等待事件…";
    byId("event-log").textContent = step
      ? `第 ${step} 次 item.updated；同一个 item ID，正文长度 ${snapshots[step - 1].length}`
      : "点击下一条累计事件开始推演。";
  }

  byId("next-event")?.addEventListener("click", () => {
    if (step >= snapshots.length) return;
    const incoming = snapshots[step];
    naive += incoming;
    safe = incoming.startsWith(safe) ? safe + incoming.slice(safe.length) : incoming;
    step += 1;
    render();
  });

  byId("reset-event")?.addEventListener("click", () => {
    step = 0;
    naive = "";
    safe = "";
    render();
  });

  document.querySelectorAll(".quiz").forEach((quiz) => {
    const feedback = quiz.querySelector(".feedback");
    quiz.querySelectorAll("button").forEach((button) => {
      button.addEventListener("click", () => {
        const correct = button.dataset.correct === "true";
        feedback.textContent = correct ? quiz.dataset.success : quiz.dataset.retry;
        feedback.className = `feedback ${correct ? "ok" : "no"}`;
      });
    });
  });

  render();
})();
