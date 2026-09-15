import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createRequire } from "node:module";
import path from "node:path";
import readline from "node:readline";

type JsonObject = Record<string, unknown>;

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

export type AppServerNotification = {
  method: string;
  params: JsonObject;
  raw: string;
};

export type AppServerTurnResult = {
  exitCode: number;
  sessionId: string;
  failureMessage?: string;
};

export class CodexAppServerLaunchError extends Error {}

type CodexAppServerTurnInput = {
  workspaceRoot: string;
  prompt: string;
  sessionId: string | null;
  signal?: AbortSignal;
  sandboxMode: "read-only" | "workspace-write";
  ephemeral?: boolean;
  environment: NodeJS.ProcessEnv;
  commandOverride?: string;
  onSession: (sessionId: string) => Promise<void>;
  onNotification: (notification: AppServerNotification) => Promise<void>;
};

/** One disposable app-server process owns one Codex turn and its interrupt path. */
export class CodexAppServerTurn {
  readonly #input: CodexAppServerTurnInput;
  #child: ChildProcessWithoutNullStreams | null = null;
  #nextRequestId = 1;
  #pending = new Map<number, PendingRequest>();
  #threadId: string | null = null;
  #turnId: string | null = null;
  #cancelled = false;
  #settled = false;
  #closing = false;
  #stderr = "";
  #lineQueue = Promise.resolve();
  #resolveTurn!: (status: { status: string; error?: string }) => void;
  #rejectTurn!: (error: Error) => void;
  readonly #turnDone = new Promise<{ status: string; error?: string }>(
    (resolve, reject) => {
      this.#resolveTurn = resolve;
      this.#rejectTurn = reject;
    },
  );

  constructor(input: CodexAppServerTurnInput) {
    this.#input = input;
    // Startup can fail before run() reaches the completion await; mark the
    // internal rejection observed while still allowing the later await to throw.
    void this.#turnDone.catch(() => undefined);
  }

  get cancelled() {
    return this.#cancelled;
  }

  async run(): Promise<AppServerTurnResult> {
    if (this.#input.signal?.aborted)
      return {
        exitCode: 130,
        sessionId: this.#input.sessionId ?? "",
      };

    const abort = () => {
      void this.cancel();
    };
    this.#input.signal?.addEventListener("abort", abort, { once: true });

    try {
      await this.#launch();
      await this.#request("initialize", {
        clientInfo: {
          name: "product-agent-workbench",
          title: "Product Agent Workbench",
          version: "0.1.0",
        },
        capabilities: { experimentalApi: true },
      });
      this.#notify("initialized");

      const threadResult = this.#input.sessionId
        ? await this.#request("thread/resume", this.#threadParams())
        : await this.#request("thread/start", this.#threadParams());
      this.#threadId = extractThreadId(threadResult);
      if (!this.#threadId)
        throw new Error(
          `Codex ${this.#input.sessionId ? "thread/resume" : "thread/start"} 未返回 thread id`,
        );
      await this.#input.onSession(this.#threadId);

      const turnResult = await this.#request("turn/start", {
        threadId: this.#threadId,
        input: [{ type: "text", text: this.#input.prompt }],
        cwd: this.#input.workspaceRoot,
        approvalPolicy: "never",
        sandboxPolicy: sandboxPolicy(
          this.#input.sandboxMode,
          this.#input.workspaceRoot,
        ),
      });
      this.#turnId = extractTurnId(turnResult) ?? this.#turnId;

      const completion = await this.#turnDone;
      const cancelled = isCancelledStatus(completion.status) || this.#cancelled;
      return {
        exitCode: cancelled ? 130 : completion.status === "completed" ? 0 : 1,
        sessionId: this.#threadId,
        ...(completion.error ? { failureMessage: completion.error } : {}),
      };
    } finally {
      this.#input.signal?.removeEventListener("abort", abort);
      await this.#shutdown();
    }
  }

  cancel() {
    if (this.#cancelled || this.#settled) return false;
    this.#cancelled = true;
    const forceTimer = setTimeout(() => this.#killProcessTree(), 2_000);
    forceTimer.unref();

    if (!this.#threadId || !this.#turnId) {
      this.#killProcessTree();
      return true;
    }

    void this.#request("turn/interrupt", {
      threadId: this.#threadId,
      turnId: this.#turnId,
    }).catch(() => this.#killProcessTree());
    return true;
  }

  #threadParams() {
    const common = {
      cwd: this.#input.workspaceRoot,
      runtimeWorkspaceRoots: [this.#input.workspaceRoot],
      approvalPolicy: "never",
      sandbox: this.#input.sandboxMode,
      developerInstructions: null,
    };
    if (this.#input.sessionId)
      return { threadId: this.#input.sessionId, ...common };
    return {
      ...common,
      ephemeral: this.#input.ephemeral ?? false,
      threadSource: "product-agent-workbench",
      experimentalRawEvents: false,
    };
  }

  async #launch() {
    const launch = resolveCodexLaunch(this.#input.commandOverride);
    let child: ChildProcessWithoutNullStreams;
    try {
      child = spawn(launch.command, [...launch.prefixArgs, "app-server", "--listen", "stdio://"], {
        cwd: this.#input.workspaceRoot,
        env: this.#input.environment,
        stdio: "pipe",
        detached: process.platform !== "win32",
      });
    } catch (error) {
      throw launchError(error);
    }
    this.#child = child;
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => {
      this.#stderr = `${this.#stderr}${chunk}`.slice(-2_000);
    });
    child.once("error", (error) => this.#fail(launchError(error)));
    child.once("close", (code, signal) => {
      if (this.#closing) return;
      const detail = this.#stderr.trim();
      this.#fail(
        new Error(
          `Codex app-server 意外退出（code=${String(code)}, signal=${String(signal)}）${detail ? `：${detail}` : ""}`,
        ),
      );
    });

    const lines = readline.createInterface({
      input: child.stdout,
      crlfDelay: Infinity,
    });
    lines.on("line", (line) => {
      this.#lineQueue = this.#lineQueue
        .then(() => this.#handleLine(line))
        .catch((error: unknown) => this.#fail(asError(error)));
    });

    await new Promise<void>((resolve, reject) => {
      const onSpawn = () => {
        cleanup();
        resolve();
      };
      const onError = (error: Error) => {
        cleanup();
        reject(launchError(error));
      };
      const cleanup = () => {
        child.off("spawn", onSpawn);
        child.off("error", onError);
      };
      child.once("spawn", onSpawn);
      child.once("error", onError);
    });
  }

  async #handleLine(line: string) {
    const trimmed = line.trim();
    if (!trimmed) return;
    let value: JsonObject;
    try {
      value = JSON.parse(trimmed) as JsonObject;
    } catch {
      throw new Error(`Codex app-server 返回非法 JSON-RPC：${trimmed.slice(0, 300)}`);
    }

    if (typeof value.id === "number" && !("method" in value)) {
      const pending = this.#pending.get(value.id);
      if (!pending) return;
      this.#pending.delete(value.id);
      clearTimeout(pending.timer);
      if (value.error && typeof value.error === "object") {
        const error = value.error as JsonObject;
        pending.reject(new Error(String(error.message ?? "JSON-RPC request failed")));
      } else {
        pending.resolve(value.result);
      }
      return;
    }

    if (typeof value.method !== "string") return;
    if (typeof value.id === "number") {
      this.#respond(value.id, approvalResponse(value.method));
      return;
    }

    const params = isObject(value.params) ? value.params : {};
    if (value.method === "turn/started")
      this.#turnId = nestedString(params, "turn", "id") ?? this.#turnId;

    await this.#input.onNotification({
      method: value.method,
      params,
      raw: trimmed,
    });

    if (value.method !== "turn/completed") return;
    const status = nestedString(params, "turn", "status") ?? "completed";
    const error = nestedString(params, "turn", "error", "message");
    this.#settled = true;
    this.#resolveTurn({ status, ...(error ? { error } : {}) });
  }

  #request(method: string, params: JsonObject): Promise<unknown> {
    const id = this.#nextRequestId++;
    return new Promise((resolve, reject) => {
      if (!this.#child?.stdin.writable) {
        reject(new Error(`Codex app-server 无法发送 ${method}`));
        return;
      }
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(new Error(`Codex app-server ${method} 超时`));
      }, this.#requestTimeout(method));
      this.#pending.set(id, { resolve, reject, timer });
      this.#child.stdin.write(
        `${JSON.stringify({ jsonrpc: "2.0", id, method, params })}\n`,
        (error) => {
          if (!error) return;
          const pending = this.#pending.get(id);
          if (pending) clearTimeout(pending.timer);
          this.#pending.delete(id);
          reject(error);
        },
      );
    });
  }

  #notify(method: string, params: JsonObject = {}) {
    this.#child?.stdin.write(
      `${JSON.stringify({ jsonrpc: "2.0", method, params })}\n`,
    );
  }

  #respond(id: number, result: unknown) {
    this.#child?.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id, result })}\n`);
  }

  #fail(error: Error) {
    if (this.#settled) return;
    this.#settled = true;
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.#pending.clear();
    this.#rejectTurn(error);
  }

  #requestTimeout(method: string) {
    if (method === "turn/interrupt") return 2_000;
    const configured = Number(
      this.#input.environment.WORKBENCH_CODEX_HANDSHAKE_TIMEOUT_MS,
    );
    if (Number.isFinite(configured) && configured > 0) return configured;
    if (method === "thread/start" || method === "thread/resume") return 60_000;
    return 30_000;
  }

  async #shutdown() {
    const child = this.#child;
    if (!child) return;
    this.#closing = true;
    if (child.exitCode !== null || child.signalCode !== null) return;
    child.stdin.end();
    await Promise.race([
      new Promise<void>((resolve) => child.once("close", () => resolve())),
      new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          this.#killProcessTree();
          resolve();
        }, 2_000);
        timer.unref();
      }),
    ]);
  }

  #killProcessTree() {
    const child = this.#child;
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    if (process.platform !== "win32" && child.pid) {
      try {
        process.kill(-child.pid, "SIGKILL");
        return;
      } catch {
        // Fall through to the direct child when process groups are unavailable.
      }
    }
    child.kill("SIGKILL");
  }
}

function resolveCodexLaunch(commandOverride?: string) {
  if (commandOverride)
    return { command: commandOverride, prefixArgs: [] as string[] };
  try {
    const moduleRequire = createRequire(import.meta.url);
    const packageJson = moduleRequire.resolve("@openai/codex/package.json");
    return {
      command: process.execPath,
      prefixArgs: [path.join(path.dirname(packageJson), "bin", "codex.js")],
    };
  } catch (error) {
    throw new CodexAppServerLaunchError(
      `Codex app-server 无法启动：未找到项目固定的 @openai/codex 运行时（${asError(error).message}）`,
    );
  }
}

function launchError(error: unknown) {
  return new CodexAppServerLaunchError(
    `Codex app-server 无法启动：${asError(error).message}`,
  );
}

function sandboxPolicy(
  mode: "read-only" | "workspace-write",
  workspaceRoot: string,
) {
  if (mode === "read-only") return { type: "readOnly", networkAccess: false };
  return {
    type: "workspaceWrite",
    writableRoots: [workspaceRoot],
    networkAccess: false,
    excludeTmpdirEnvVar: false,
    excludeSlashTmp: false,
  };
}

function approvalResponse(method: string) {
  if (/requestApproval|Approval/.test(method)) return { decision: "decline" };
  return {};
}

function extractThreadId(value: unknown) {
  if (!isObject(value)) return null;
  return nestedString(value, "thread", "id") ?? stringValue(value.threadId);
}

function extractTurnId(value: unknown) {
  if (!isObject(value)) return null;
  return nestedString(value, "turn", "id") ?? stringValue(value.turnId);
}

function nestedString(value: JsonObject, ...keys: string[]): string | null {
  let current: unknown = value;
  for (const key of keys) {
    if (!isObject(current)) return null;
    current = current[key];
  }
  return stringValue(current);
}

function stringValue(value: unknown) {
  return typeof value === "string" && value ? value : null;
}

function isObject(value: unknown): value is JsonObject {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isCancelledStatus(status: string) {
  return ["cancelled", "canceled", "aborted", "interrupted"].includes(status);
}

function asError(error: unknown) {
  return error instanceof Error ? error : new Error(String(error));
}
