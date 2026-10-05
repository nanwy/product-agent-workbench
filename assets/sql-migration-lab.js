(() => {
  const scenarios = {
    valid: {
      title: "合法 Requirement",
      row: `id="req-1" · project_id="project-1" · title="增加搜索" · stage="grilling" · revision=1`,
      verdict: "accepted",
      message: "数据库接受：身份、必填值、阶段和版本都满足约束。",
    },
    blank: {
      title: "缺少 project_id",
      row: `id="req-2" · project_id=NULL · title="增加搜索" · stage="grilling" · revision=1`,
      verdict: "rejected",
      message: "数据库拒绝：NOT NULL 不允许 Requirement 失去所属项目。",
    },
    revision: {
      title: "revision 从 0 开始",
      row: `id="req-3" · project_id="project-1" · title="增加搜索" · stage="grilling" · revision=0`,
      verdict: "rejected",
      message: "数据库拒绝：CHECK(revision >= 1) 防止不存在的版本进入持久层。",
    },
    stage: {
      title: "拼错 stage",
      row: `id="req-4" · project_id="project-1" · title="增加搜索" · stage="griling" · revision=1`,
      verdict: "rejected",
      message: "数据库拒绝：CHECK(stage IN (...)) 把领域枚举变成持久化边界。",
    },
    duplicate: {
      title: "重复 id",
      row: `id="req-1" · project_id="project-2" · title="另一个需求" · stage="spec" · revision=1`,
      verdict: "rejected",
      message: "数据库拒绝：PRIMARY KEY 防止第二条事实冒用已有 Requirement 身份。",
    },
  };

  const gate = document.querySelector("[data-database-gate]");
  const scenarioButtons = document.querySelectorAll("[data-scenario]");
  scenarioButtons.forEach((button) => {
    button.addEventListener("click", () => {
      scenarioButtons.forEach((item) => item.classList.remove("active"));
      button.classList.add("active");
      const scenario = scenarios[button.dataset.scenario];
      gate.innerHTML = `
        <strong>${scenario.title}</strong>
        <p><code>${scenario.row}</code></p>
        <p class="${scenario.verdict}">${scenario.message}</p>
      `;
    });
  });

  const editor = document.querySelector("[data-sql-editor]");
  const results = document.querySelector("[data-check-results]");
  const checkButton = document.querySelector("[data-check-sql]");
  const resetButton = document.querySelector("[data-reset-sql]");
  const hint = document.querySelector("[data-progressive-hint]");
  const starter = editor?.value ?? "";
  let attempts = 0;

  const includesAll = (source, values) => values.every((value) => source.includes(`'${value}'`));
  const lineHas = (source, field, tokens) => {
    const line = source.split("\n").find((item) => new RegExp(`^\\s*${field}\\b`).test(item));
    return Boolean(line && tokens.every((token) => line.includes(token)));
  };

  checkButton?.addEventListener("click", () => {
    attempts += 1;
    const sql = editor.value.toLowerCase().replace(/\s+/g, " ");
    const lineSQL = editor.value.toLowerCase();
    const checks = [
      ["创建 requirements 表", /create\s+table\s+requirements\s*\(/.test(sql)],
      ["id 是 TEXT、显式 NOT NULL、PRIMARY KEY", lineHas(lineSQL, "id", ["text", "not null", "primary key"])],
      ["project_id 是必填 TEXT", lineHas(lineSQL, "project_id", ["text", "not null"])],
      ["title 是必填 TEXT", lineHas(lineSQL, "title", ["text", "not null"])],
      ["stage 是必填 TEXT", lineHas(lineSQL, "stage", ["text", "not null"])],
      ["revision 是必填 INTEGER", lineHas(lineSQL, "revision", ["integer", "not null"])],
      ["revision 至少为 1", /check\s*\(\s*revision\s*>=\s*1\s*\)/.test(sql)],
      [
        "stage 包含六个领域值",
        /check\s*\(\s*stage\s+in\s*\(/.test(sql) && includesAll(sql, [
          "grilling", "spec", "tickets", "implementation", "acceptance", "delivered",
        ]),
      ],
      ["语句以分号结束", /;\s*$/.test(editor.value)],
    ];
    const passed = checks.filter(([, ok]) => ok).length;
    results.hidden = false;
    results.innerHTML = `
      <strong>${passed === checks.length ? "通过：可以交给 Agent review" : `完成 ${passed}/${checks.length} 项`}</strong>
      <ul>${checks.map(([label, ok]) => `<li class="${ok ? "pass" : "fail"}">${ok ? "✓" : "○"} ${label}</li>`).join("")}</ul>
    `;
    hint.hidden = passed === checks.length;
    if (passed !== checks.length) {
      hint.textContent = attempts === 1
        ? "第一层提示：先让五个字段都有类型和 NOT NULL，再处理两个 CHECK。"
        : "第二层提示：CHECK 可以写在所有字段之后；字符串必须使用单引号。";
    }
  });

  resetButton?.addEventListener("click", () => {
    editor.value = starter;
    results.hidden = true;
    hint.hidden = true;
    attempts = 0;
  });
})();

