import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type {
  AgentBackend,
  AgentEvent,
  AgentRunOptions,
  AgentRunResult,
} from "@neko/llm";

type McpServerLike = {
  name?: string;
  instance: {
    connect(transport: ReturnType<typeof InMemoryTransport.createLinkedPair>[1]): Promise<void>;
  };
};

export type ScriptedToolResult = {
  isError: boolean;
  content: unknown;
};

export type ScriptedEvalContext = {
  options: AgentRunOptions;
  call(server: string, tool: string, args?: Record<string, unknown>): Promise<ScriptedToolResult>;
  listTools(server: string): Promise<string[]>;
  readSkill(skillName: string): Promise<{ body: string; contentHash: string }>;
  emit(event: AgentEvent): Promise<void>;
};

export type ScriptedEvalProgram = (
  context: ScriptedEvalContext,
) => Promise<string> | string;

async function emit(options: AgentRunOptions, event: AgentEvent): Promise<void> {
  await options.onEvent?.(event);
}
function serverHandle(value: unknown, name: string): McpServerLike {
  if (
    !value ||
    typeof value !== "object" ||
    !("instance" in value) ||
    !value.instance ||
    typeof value.instance !== "object" ||
    !("connect" in value.instance) ||
    typeof value.instance.connect !== "function"
  ) {
    throw new Error(`scripted backend received invalid MCP server ${name}`);
  }
  return value as McpServerLike;
}

/**
 * Provider-free backend used only to prove the eval harness. It talks to the
 * same in-process MCP servers that a production backend receives from
 * runAgentBackend. The program is deterministic and is never a ranked model
 * candidate.
 */
export class ScriptedEvalBackend implements AgentBackend {
  readonly id: AgentBackend["id"];
  readonly model = "deterministic-v1";
  readonly capabilities = {
    mcpTools: true,
    sessionResume: false,
  } as const;

  constructor(
    candidateId: string,
    private readonly program: ScriptedEvalProgram,
  ) {
    // Product registration remains closed; the eval driver supplies this
    // backend directly through RunChatTurnDeps and records candidate identity
    // separately. The DB column and wire protocol are string-valued.
    this.id = candidateId as AgentBackend["id"];
  }

  async run(options: AgentRunOptions): Promise<AgentRunResult> {
    const clients = new Map<string, Client>();
    let callSequence = 0;
    const getClient = async (name: string): Promise<Client> => {
      const existing = clients.get(name);
      if (existing) return existing;
      const raw = options.mcpServers?.[name];
      if (!raw) throw new Error(`scripted backend requires MCP server ${name}`);
      const server = serverHandle(raw, name);
      const [clientTransport, serverTransport] =
        InMemoryTransport.createLinkedPair();
      await server.instance.connect(serverTransport);
      const client = new Client({
        name: `openneko-eval-${name}`,
        version: "1.0.0",
      });
      await client.connect(clientTransport);
      clients.set(name, client);
      return client;
    };

    const context: ScriptedEvalContext = {
      options,
      emit: (event) => emit(options, event),
      async listTools(server) {
        if (options.signal?.aborted) throw options.signal.reason;
        const client = await getClient(server);
        const result = await client.listTools();
        return result.tools.map((tool) => tool.name).sort();
      },
      async call(server, tool, args = {}) {
        if (options.signal?.aborted) throw options.signal.reason;
        callSequence += 1;
        const id = `scripted-tool-${callSequence}`;
        const name = `mcp_${server}_${tool}`;
        await emit(options, { type: "tool_start", id, name, input: args });
        try {
          const client = await getClient(server);
          const result = await client.callTool({ name: tool, arguments: args });
          await emit(options, {
            type: "tool_end",
            id,
            result: {
              isError: result.isError === true,
              content: result.content,
            },
          });
          return {
            isError: result.isError === true,
            content: result.content,
          };
        } catch (cause) {
          const message = cause instanceof Error ? cause.message : String(cause);
          await emit(options, { type: "tool_end", id, error: message });
          throw cause;
        }
      },
      async readSkill(skillName) {
        if (!options.workspace) throw new Error("scripted backend has no workspace");
        const path = `${options.workspace.skillsRoot}/${skillName}/SKILL.md`;
        callSequence += 1;
        const id = `scripted-tool-${callSequence}`;
        await emit(options, {
          type: "tool_start",
          id,
          name: "read_file",
          input: { path },
        });
        try {
          const body = await readFile(path, "utf8");
          const contentHash = createHash("sha256").update(body).digest("hex");
          await emit(options, {
            type: "tool_end",
            id,
            result: { bytes: Buffer.byteLength(body, "utf8"), contentHash },
          });
          return { body, contentHash };
        } catch (cause) {
          const message = cause instanceof Error ? cause.message : String(cause);
          await emit(options, { type: "tool_end", id, error: message });
          throw cause;
        }
      },
    };

    try {
      const finalText = await this.program(context);
      await emit(options, {
        type: "message",
        role: "assistant",
        content: finalText,
      });
      await emit(options, {
        type: "usage",
        source: "outer",
        provider: "scripted",
        model: this.model,
        usage: {
          coverage: "complete",
          inputTokens: 0,
          outputTokens: 0,
          totalTokens: 0,
        },
      });
      return { finalText, status: "completed", threadState: {} };
    } finally {
      await Promise.allSettled([...clients.values()].map((client) => client.close()));
    }
  }
}
