import test from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import { existsSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import { withChromeStartup } from "./test-release-flow.mjs";

const target = [{ type: "page", webSocketDebuggerUrl: "ws://127.0.0.1:9223/devtools/page/test" }];
const response = (value = target) => ({ ok: true, json: async () => value });
class Socket extends EventTarget {
  constructor() { super(); queueMicrotask(() => this.dispatchEvent(new Event("open"))); }
  close() { this.closed = true; }
}

function fixture(t, overrides = {}) {
  const root = mkdtempSync(join(tmpdir(), "release-startup-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const child = new EventEmitter();
  child.pid = 123456;
  child.stdout = new PassThrough();
  child.stderr = new PassThrough();
  child.kills = [];
  const emit = child.emit.bind(child);
  child.emit = (event, ...args) => {
    const result = emit(event, ...args);
    if (event === "exit") queueMicrotask(() => emit("close", ...args));
    return result;
  };
  child.kill = (signal) => {
    child.kills.push(signal);
    queueMicrotask(() => child.emit("exit", null, signal));
    return true;
  };
  const state = { child, root, spawnCalls: 0, fetchCalls: 0, socket: null };
  state.options = {
    env: { CHROME_BIN: "/fake/chrome", CI: "1" },
    startupMs: 90, requestMs: 15, pollMs: 1, termMs: 15, killMs: 15,
    profileRoot: root,
    probePort: async () => "free",
    probeVersion: () => ({ output: "Test Chrome 1", status: 0 }),
    spawnChrome: (command, args, settings) => {
      state.spawnCalls++;
      state.command = command;
      state.args = args;
      state.settings = settings;
      return child;
    },
    fetchReady: async () => { state.fetchCalls++; return response(); },
    WebSocketClass: class extends Socket { constructor() { super(); state.socket = this; } },
    ...overrides,
  };
  return state;
}
async function failed(state) {
  let error;
  await assert.rejects(withChromeStartup(() => assert.fail("journey must not run"), state.options), (value) => {
    error = value;
    return true;
  });
  assert.deepEqual(readdirSync(state.root), [], "temporary profile removed before rejection");
  const diagnostic = JSON.parse(error.message.split("Chrome startup diagnostics: ")[1]);
  assert.equal(diagnostic.cleanup, "process stopped; profile removed");
  return { error, diagnostic };
}

test("spawn error is immediate, classified, and removes profile", async (t) => {
  const state = fixture(t);
  state.options.spawnChrome = () => {
    state.child.pid = undefined;
    queueMicrotask(() => state.child.emit("error", Object.assign(new Error("missing"), { code: "ENOENT" })));
    return state.child;
  };
  const { error, diagnostic } = await failed(state);
  assert.match(error.message, /spawn-error \(ENOENT\)/);
  assert.equal(diagnostic.pid, null);
  assert.deepEqual(state.child.kills, []);
});

test("synchronous spawn failure also removes profile", async (t) => {
  const state = fixture(t, { spawnChrome: () => { throw new Error("spawn threw"); } });
  assert.match((await failed(state)).error.message, /spawn threw/);
});

test("immediate nonzero exit reports status and stderr without exhausting startup budget", async (t) => {
  const state = fixture(t, { startupMs: 10000 });
  state.options.fetchReady = () => {
    state.child.stderr.write("Chrome sandbox initialization failed\n");
    queueMicrotask(() => state.child.emit("exit", 23, null));
    return new Promise(() => {});
  };
  const { error, diagnostic } = await failed(state);
  assert.match(error.message, /early-exit/);
  assert.equal(diagnostic.exitCode, 23);
  assert.match(diagnostic.stderrTail, /sandbox initialization failed/);
  assert.ok(diagnostic.elapsedMs < 1000);
  assert.deepEqual(state.child.kills, []);
});

test("signal termination is distinct from nonzero exit", async (t) => {
  const state = fixture(t);
  state.options.fetchReady = async () => {
    queueMicrotask(() => state.child.emit("exit", null, "SIGABRT"));
    return response();
  };
  const { diagnostic } = await failed(state);
  assert.equal(diagnostic.signal, "SIGABRT");
  assert.equal(diagnostic.exitCode, null);
});

test("early close without exit is observed", async (t) => {
  const state = fixture(t);
  state.options.fetchReady = async () => {
    queueMicrotask(() => state.child.emit("close", 7, null));
    return response();
  };
  const { diagnostic } = await failed(state);
  assert.equal(diagnostic.processEvent, "early-close");
  assert.equal(diagnostic.exitCode, 7);
});

test("delayed readiness tolerates startup failures, preserves flags, cleans socket/process/profile", async (t) => {
  const state = fixture(t, { probePort: async () => "occupied", startupMs: 500 });
  let attempts = 0, ran = false;
  state.options.fetchReady = async () => {
    if (++attempts < 3) throw new Error("not listening yet");
    return response();
  };
  await withChromeStartup(async (ws, diagnostic) => {
    ran = true;
    assert.equal(ws, state.socket);
    assert.equal(diagnostic.portBeforeSpawn, "occupied", "a preexisting listener is diagnostic only");
    assert.equal(diagnostic.lastReadinessFailure, "connection/fetch-failure");
  }, state.options);
  assert.equal(ran, true);
  assert.equal(state.spawnCalls, 1, "no process retries");
  assert.equal(attempts, 3);
  assert.equal(state.socket.closed, true);
  assert.deepEqual(state.child.kills, ["SIGTERM"]);
  assert.deepEqual(readdirSync(state.root), []);
  assert.deepEqual(state.settings.stdio, ["ignore", "pipe", "pipe"]);
  assert.deepEqual(state.args, [
    "--headless=new", "--remote-debugging-port=9223", state.args[2],
    "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions", "--no-sandbox", "about:blank",
  ]);
  assert.match(state.args[2], /--user-data-dir=.*release-smoke-chrome-/);
});

for (const [name, fetchReady, category] of [
  ["connection failure", async () => { throw new Error("refused"); }, "connection/fetch-failure"],
  ["HTTP non-success", async () => ({ ok: false, status: 503 }), "http-non-success (503)"],
  ["malformed JSON", async () => ({ ok: true, json: async () => { throw new SyntaxError("bad JSON"); } }), "malformed-json"],
  ["no page target", async () => response([{ type: "worker" }]), "response-without-page-target"],
  ["wrong JSON shape", async () => response({ targets: target }), "response-without-page-target"],
  ["page without websocket", async () => response([{ type: "page" }]), "response-without-page-target"],
]) {
  test(`${name} retains its distinct readiness category and cleans up`, async (t) => {
    const state = fixture(t, { fetchReady });
    const { error, diagnostic } = await failed(state);
    assert.match(error.message, /readiness budget exhausted/);
    assert.equal(diagnostic.lastReadinessFailure, category);
    assert.deepEqual(state.child.kills, ["SIGTERM"]);
  });
}

test("stalled fetch is aborted and bounded even if fetch ignores abort", async (t) => {
  let aborted = 0, attempts = 0;
  const state = fixture(t, { fetchReady: (_, { signal }) => {
    attempts++;
    signal.addEventListener("abort", () => aborted++);
    return new Promise(() => {});
  } });
  const { diagnostic } = await failed(state);
  assert.equal(diagnostic.lastReadinessFailure, "timeout/stalled-request");
  assert.equal(aborted, attempts);
  assert.ok(attempts > 1, "each request has its own timeout");
  assert.ok(diagnostic.elapsedMs < 1000);
});

test("stalled response body cannot exceed overall budget", async (t) => {
  const state = fixture(t, {
    requestMs: 10000,
    fetchReady: async () => ({ ok: true, json: () => new Promise(() => {}) }),
  });
  const { diagnostic } = await failed(state);
  assert.equal(diagnostic.lastReadinessFailure, "timeout/stalled-request");
  assert.ok(diagnostic.elapsedMs < 1000);
});

test("stdout and stderr retain only bounded tails, including for a huge chunk", async (t) => {
  const state = fixture(t);
  state.options.fetchReady = async () => {
    state.child.stdout.write("discard".repeat(100000));
    state.child.stdout.write("stdout-tail");
    for (let i = 0; i < 100; i++) state.child.stderr.write("discard".repeat(100));
    state.child.stderr.write("stderr-tail");
    queueMicrotask(() => state.child.emit("exit", 1, null));
    return response();
  };
  const { diagnostic } = await failed(state);
  for (const stream of ["stdout", "stderr"]) {
    assert.equal(Buffer.byteLength(diagnostic[`${stream}Tail`]), 8192);
    assert.ok(diagnostic[`${stream}Tail`].endsWith(`${stream}-tail`));
  }
});

for (const mode of ["constructor", "error", "close", "stall"]) {
  test(`WebSocket ${mode} failure closes socket and removes profile`, async (t) => {
    let socket;
    const state = fixture(t, { WebSocketClass: class extends EventTarget {
      constructor() {
        super();
        if (mode === "constructor") throw new Error("constructor failure");
        socket = this;
        if (mode !== "stall") queueMicrotask(() => this.dispatchEvent(new Event(mode)));
      }
      close() { this.closed = true; }
    } });
    const { error } = await failed(state);
    assert.match(error.message, /WebSocket setup failed/);
    if (socket) assert.equal(socket.closed, true);
    assert.deepEqual(state.child.kills, ["SIGTERM"]);
  });
}

test("SIGTERM timeout escalates only the spawned child to SIGKILL and waits before removing profile", async (t) => {
  const state = fixture(t);
  state.child.kill = (signal) => {
    state.child.kills.push(signal);
    assert.equal(readdirSync(state.root).length, 1, "profile retained while process is alive");
    if (signal === "SIGKILL") queueMicrotask(() => state.child.emit("exit", null, signal));
    return true;
  };
  await withChromeStartup(() => {}, state.options);
  assert.deepEqual(state.child.kills, ["SIGTERM", "SIGKILL"]);
  assert.deepEqual(readdirSync(state.root), []);
});

test("unconfirmed process death is a bounded cleanup failure and retains profile safely", async (t) => {
  const state = fixture(t);
  state.child.kill = (signal) => { state.child.kills.push(signal); return true; };
  await assert.rejects(withChromeStartup(() => {}, state.options), /cleanup failed.*profile retained/);
  assert.equal(readdirSync(state.root).length, 1);
  assert.deepEqual(state.child.kills, ["SIGTERM", "SIGKILL"]);
});

test("journey assertion errors still propagate after cleanup", async (t) => {
  const state = fixture(t);
  await assert.rejects(withChromeStartup(() => { throw new Error("release assertion failed"); }, state.options), /release assertion failed/);
  assert.equal(state.socket.closed, true);
  assert.deepEqual(state.child.kills, ["SIGTERM"]);
  assert.deepEqual(readdirSync(state.root), []);
});

test("version probe failure is diagnostic, command resolves to actual executable, port is recorded", async (t) => {
  const state = fixture(t, {
    env: { CHROME_BIN: "node", PATH: process.env.PATH },
    probeVersion: () => { throw Object.assign(new Error("not available"), { code: "EACCES" }); },
    probePort: async () => "occupied",
  });
  state.options.fetchReady = async () => {
    queueMicrotask(() => state.child.emit("exit", 1, null));
    return response();
  };
  const { diagnostic } = await failed(state);
  assert.equal(diagnostic.command, "node");
  assert.ok(diagnostic.executable.startsWith("/"));
  assert.equal(diagnostic.version.error, "EACCES");
  assert.equal(diagnostic.portBeforeSpawn, "occupied");
  assert.equal(diagnostic.host, "127.0.0.1");
  assert.equal(diagnostic.port, 9223);
  assert.equal(diagnostic.pid, state.child.pid);
});

test("version probe failure does not prevent successful startup", async (t) => {
  const state = fixture(t, { probeVersion: () => { throw new Error("unavailable"); } });
  await withChromeStartup(() => {}, state.options);
  assert.equal(state.spawnCalls, 1);
});

test("diagnostics redact configured smoke credentials without dumping environment", async (t) => {
  const state = fixture(t, { env: {
    CHROME_BIN: "/fake/chrome", SMOKE_EMAIL: "private@example.test", SMOKE_PASSWORD: 'private\\"password', OTHER_SECRET: "never-print-env",
  } });
  state.options.fetchReady = async () => {
    state.child.stderr.write(`${state.options.env.SMOKE_EMAIL} ${state.options.env.SMOKE_PASSWORD}`);
    queueMicrotask(() => state.child.emit("exit", 1, null));
    return response();
  };
  const { error } = await failed(state);
  assert.ok(!error.message.includes("private"));
  assert.ok(!error.message.includes("never-print-env"));
  assert.match(error.message, /REDACTED/);
});

test("real /bin/false fails quickly with code 1 and removes its actual mkdtemp profile", async (t) => {
  assert.ok(existsSync("/bin/false"));
  const state = fixture(t, {
    env: { CHROME_BIN: "/bin/false" }, spawnChrome: spawn,
    startupMs: 10000, requestMs: 500,
    fetchReady: () => new Promise(() => {}),
  });
  const { error, diagnostic } = await failed(state);
  assert.match(error.message, /early-exit/);
  assert.equal(diagnostic.executable, "/bin/false");
  assert.equal(diagnostic.exitCode, 1);
  assert.ok(diagnostic.elapsedMs < 1000, `${diagnostic.elapsedMs}ms instead of 10 seconds`);
  assert.equal(existsSync(diagnostic.profile), false);
});


test("default selection finds an executable on PATH without spawning discovery commands", async (t) => {
  const state = fixture(t);
  writeFileSync(join(state.root, "chromium"), "#!/bin/sh\n", { mode: 0o700 });
  state.options.env = { PATH: state.root };
  await withChromeStartup((ws, diagnostic) => {
    assert.equal(diagnostic.command, "chromium");
    assert.equal(diagnostic.executable, join(state.root, "chromium"));
  }, state.options);
  assert.equal(state.spawnCalls, 1);
  assert.deepEqual(readdirSync(state.root), ["chromium"]);
});

test("missing Chrome command is explicit and creates neither child nor profile", async (t) => {
  const state = fixture(t, { env: { PATH: "" } });
  const { error } = await failed(state);
  assert.match(error.message, /Chrome\/Chromium not found/);
  assert.equal(state.spawnCalls, 0);
});

test("a process error after PID allocation cannot falsely prove death for cleanup", async (t) => {
  const state = fixture(t);
  state.options.fetchReady = async () => {
    queueMicrotask(() => state.child.emit("error", Object.assign(new Error("process failure"), { code: "EIO" })));
    return response();
  };
  const { error } = await failed(state);
  assert.match(error.message, /spawn-error \(EIO\)/);
  assert.deepEqual(state.child.kills, ["SIGTERM"]);
});

test("Chrome death interrupts a stalled WebSocket immediately", async (t) => {
  const state = fixture(t, { startupMs: 10000 });
  state.options.WebSocketClass = class extends EventTarget {
    constructor() {
      super();
      queueMicrotask(() => state.child.emit("exit", 9, null));
    }
    close() { state.socketClosed = true; }
  };
  const { error, diagnostic } = await failed(state);
  assert.match(error.message, /WebSocket setup failed.*early-exit/);
  assert.equal(diagnostic.exitCode, 9);
  assert.ok(diagnostic.elapsedMs < 1000);
  assert.equal(state.socketClosed, true);
});

test("real SIGTERM-resistant child is killed by PID and its profile is removed", async (t) => {
  let child, ready;
  const initialized = new Promise((resolveReady) => { ready = resolveReady; });
  const signals = [];
  const state = fixture(t, {
    startupMs: 2000, termMs: 30, killMs: 1000,
    spawnChrome: () => {
      // No descendants: the shell spins only for the short SIGTERM grace period.
      child = spawn("/bin/sh", ["-c", 'trap "" TERM; printf ready; while :; do :; done'], {
        stdio: ["ignore", "pipe", "pipe"],
      });
      const kill = child.kill.bind(child);
      child.kill = (signal) => { signals.push({ pid: child.pid, signal }); return kill(signal); };
      child.stdout.once("data", ready);
      return child;
    },
    fetchReady: async () => { await initialized; return response(); },
  });
  t.after(() => {
    if (child && child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
  });
  await withChromeStartup(() => {}, state.options);
  assert.deepEqual(signals, [{ pid: child.pid, signal: "SIGTERM" }, { pid: child.pid, signal: "SIGKILL" }]);
  assert.equal(child.signalCode, "SIGKILL");
  assert.throws(() => process.kill(child.pid, 0), { code: "ESRCH" });
  assert.deepEqual(readdirSync(state.root), []);
});

test("cleanup waits for close and captures stderr delivered after exit", async (t) => {
  const state = fixture(t);
  state.child.emit = EventEmitter.prototype.emit;
  state.options.fetchReady = () => {
    queueMicrotask(() => {
      state.child.emit("exit", 42, null);
      setImmediate(() => {
        assert.equal(readdirSync(state.root).length, 1, "profile retained until close");
        state.child.stderr.write("last stderr bytes after exit");
        state.child.emit("close", 42, null);
      });
    });
    return new Promise(() => {});
  };
  const { diagnostic } = await failed(state);
  assert.match(diagnostic.stderrTail, /last stderr bytes after exit/);
  assert.equal(diagnostic.exitCode, 42);
});

test("exit without closed stdio produces a bounded cleanup failure and retains profile", async (t) => {
  const state = fixture(t);
  state.child.emit = EventEmitter.prototype.emit;
  state.options.fetchReady = async () => {
    queueMicrotask(() => state.child.emit("exit", 1, null));
    return response();
  };
  await assert.rejects(withChromeStartup(() => assert.fail("journey must not run"), state.options), /Chrome stdio did not close; profile retained/);
  assert.equal(readdirSync(state.root).length, 1);
  assert.deepEqual(state.child.kills, []);
});
