/**
 * mcp-code-search Pi Extension
 *
 * Registers three Pi custom tools backed by the mcp-code-search MCP server:
 *   - index_codebase(directory) — index a codebase
 *   - index_status()            — show index state
 *   - search_code(query, ...)   — semantic code search
 *
 * ## Setup
 *
 * 1. Build the binary:
 *    cd /path/to/semantic-codesearch && go build -o mcp-code-search .
 *
 * 2. Place this extension where Pi can find it:
 *    cp pi-extension/index.ts ~/.pi/agent/extensions/codesearch.ts
 *    # OR load it directly:
 *    pi -e ./pi-extension/index.ts
 *
 * 3. The binary is auto-detected. Set MCP_CS_BINARY if you put it somewhere
 *    else:  export MCP_CS_BINARY=/path/to/mcp-code-search
 *
 * 4. Use it in Pi:
 *    pi
 *    > index my project
 *    > search for "authentication middleware"
 *    > find "rate limiter" across all codebases
 */

import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

// ── Locate the binary ──────────────────────────────────────────────────

const __dirname = dirname(fileURLToPath(import.meta.url));

function findBinary(cwd: string): string | null {
  // 1) env var
  const envBin = process.env.MCP_CS_BINARY;
  if (envBin) {
    if (existsSync(envBin)) return envBin;
    // treat as bare command name (on PATH)
    return envBin;
  }

  // 2) relative to the extension source dir
  const rel = resolve(__dirname, "..", "mcp-code-search");
  if (existsSync(rel)) return rel;

  // 3) relative to process cwd (for `pi -e pi-extension/index.ts` from project root)
  const cwdBin = resolve(cwd, "mcp-code-search");
  if (existsSync(cwdBin)) return cwdBin;

  // 4) on PATH
  return "mcp-code-search";
}

// ── Minimal JSON-RPC 2.0 client over a stdio subprocess ────────────────

class MCPClient {
  private proc: ChildProcess | null = null;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private buf = "";
  private nextId = 1;
  private _closed = false;

  get closed(): boolean {
    return this._closed;
  }

  async start(bin: string): Promise<void> {
    this.proc = spawn(bin, ["serve"], {
      stdio: ["pipe", "pipe", "inherit"],
      env: { ...process.env },
    });

    this.proc.stdout!.on("data", (chunk: Buffer) => {
      this.buf += chunk.toString();
      this._drain();
    });

    this.proc.on("exit", (code) => {
      this._closed = true;
      this._rejectAll(new Error(`MCP server exited with code ${code}`));
    });

    this.proc.on("error", (err) => {
      this._closed = true;
      this._rejectAll(err);
    });
  }

  private _drain() {
    const lines = this.buf.split("\n");
    this.buf = lines.pop() ?? "";
    for (const raw of lines) {
      const t = raw.trim();
      if (!t) continue;
      try {
        const msg = JSON.parse(t);
        if (msg.id != null && this.pending.has(msg.id)) {
          const { resolve, reject } = this.pending.get(msg.id)!;
          this.pending.delete(msg.id);
          if (msg.error) reject(new Error(msg.error.message ?? "MCP error"));
          else resolve(msg.result);
        }
      } catch { /* ignore partial lines */ }
    }
  }

  private _rejectAll(err: Error) {
    for (const [, entry] of this.pending) entry.reject(err);
    this.pending.clear();
  }

  async call(method: string, params: Record<string, unknown> = {}): Promise<unknown> {
    if (this._closed) throw new Error("MCP server closed");
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.proc?.stdin?.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
    });
  }

  stop() {
    this._closed = true;
    this.proc?.stdin?.end();
    this.proc?.kill();
    this.proc = null;
    this._rejectAll(new Error("disconnected"));
  }
}

// ── Build a TypeBox schema from MCP inputSchema ────────────────────────

function schemaFromProps(props: Record<string, unknown>, required: string[]) {
  const fields: Record<string, ReturnType<typeof Type.String>> = {};
  for (const [k, v] of Object.entries(props)) {
    const p = v as { type?: string; description?: string };
    const desc = p.description ?? "";
    switch (p.type) {
      case "number":
      case "integer":
        fields[k] = Type.Number({ description: desc });
        break;
      case "boolean":
        fields[k] = Type.Boolean({ description: desc });
        break;
      default:
        fields[k] = Type.String({ description: desc });
    }
  }
  return Type.Object(fields, { additionalProperties: false });
}

// ── Extension entry point ──────────────────────────────────────────────

export default async function (pi: ExtensionAPI) {
  const cwd = process.cwd();
  const binary = findBinary(cwd);

  if (!binary || (!existsSync(binary) && !binary.includes("/") && !binary.includes("\\"))) {
    pi.on("session_start", async (_event, ctx) => {
      ctx.ui.notify(
        "🔍 codesearch: binary not found. Set MCP_CS_BINARY or place mcp-code-search on PATH.",
        "warning",
      );
    });
    return;
  }

  // Start the MCP server subprocess
  const mcp = new MCPClient();
  try {
    await mcp.start(binary);
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    pi.on("session_start", async (_event, ctx) => {
      ctx.ui.notify(`🔍 codesearch: failed to start — ${msg}`, "error");
    });
    return;
  }

  // Fetch tool definitions from the MCP server
  let mcpTools: Array<{ name: string; description: string; inputSchema: { properties?: Record<string, unknown>; required?: string[] } }>;
  try {
    const result = (await mcp.call("tools/list")) as { tools: typeof mcpTools };
    mcpTools = result.tools;
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    mcp.stop();
    pi.on("session_start", async (_event, ctx) => {
      ctx.ui.notify(`🔍 codesearch: tools/list failed — ${msg}`, "error");
    });
    return;
  }

  // Register each MCP tool as a Pi tool
  for (const t of mcpTools) {
    const props = t.inputSchema?.properties ?? {};
    const req = t.inputSchema?.required ?? [];
    const parameters = schemaFromProps(props, req);

    pi.registerTool({
      name: t.name,
      label: t.name.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()),
      description: t.description,
      promptSnippet: "Search code with natural language queries or index a codebase",
      promptGuidelines: [
        `Use ${t.name} when you need semantic understanding of code rather than exact text matching.`,
      ],
      parameters,
      async execute(_toolCallId, params, _signal, _onUpdate, _ctx) {
        const result = await mcp.call(t.name, params as Record<string, unknown>);
        const text = JSON.stringify(result, null, 2);
        return {
          content: [{ type: "text", text }],
          details: { raw: result },
        };
      },
    } as ReturnType<typeof pi.registerTool> extends (def: infer D) => void ? D : never);
  }

  // Cleanup
  pi.on("session_shutdown", () => mcp.stop());

  pi.on("session_start", async (_event, ctx) => {
    ctx.ui.notify(`🔍 Code search loaded (${mcpTools.length} tools)`, "info");
  });
}
