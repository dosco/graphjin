import { describe, expect, it } from "vitest";
import type { AgentWorkspace } from "../src/agent-backend";
import type { KnowledgePackContents } from "../src/knowledge-pack";
import type { PluginCatalog } from "../src/work/control-plane";
import { buildBotSection, buildWorkPrompt } from "../src/work/prompt";
import { VALUE_ESTIMATE_INSTRUCTIONS } from "../src/prompts/sections";

const workspace: AgentWorkspace = {
  orgRoot: "/tmp/org",
  skillsRoot: "/tmp/org/skills",
  memoryRoot: "/tmp/org/memory",
  knowledgeRoot: "/tmp/org/knowledge",
  uploadsRoot: "/tmp/org/uploads",
  runsRoot: "/tmp/org/runs",
  threadUploadsRoot: "/tmp/org/uploads/t1",
  runRoot: "/tmp/org/runs/r1",
  artifactRoot: "/tmp/org/runs/r1/artifacts",
  binRoot: "/tmp/org/runs/r1/bin",
};

const knowledge: KnowledgePackContents = {
  tables: "{}",
  namespaces: "{}",
  insights: "{}",
  syntax: "{}",
};

function build(
  backend: "hermes" | "ax",
  overrides: {
    wantsCards?: boolean;
    includeUxMetadata?: boolean;
    supportsCardTool?: boolean;
    supportsSkillTool?: boolean;
    supportsMemoryTool?: boolean;
    supportsWorkflowTool?: boolean;
    supportsPolicyTool?: boolean;
    supportsSourceConfigTool?: boolean;
    supportsClarificationTool?: boolean;
    supportsPluginManagerTool?: boolean;
    supportsNativeDelegation?: boolean;
    pluginCatalog?: PluginCatalog;
    installedSkills?: Array<{ name: string; description: string }>;
    pluginActions?: Array<{
      kind: string;
      description: string;
      scope?: "external" | "internal";
      default_mode?: "auto" | "ask" | "deny";
    }>;
    dataSurface?: "customer" | "records";
    graphjinDataPath?: "direct" | "agent";
  } = {},
): string {
  return buildWorkPrompt({
    backend,
    workspace,
    knowledge,
    messages: [],
    currentUserMessage: "test",
    wantsCards: overrides.wantsCards ?? true,
    includeUxMetadata: overrides.includeUxMetadata,
    supportsCardTool: overrides.supportsCardTool ?? false,
    supportsSkillTool: overrides.supportsSkillTool ?? false,
    supportsMemoryTool: overrides.supportsMemoryTool ?? false,
    supportsWorkflowTool: overrides.supportsWorkflowTool ?? false,
    supportsPolicyTool: overrides.supportsPolicyTool ?? false,
    supportsSourceConfigTool: overrides.supportsSourceConfigTool ?? false,
    supportsClarificationTool: overrides.supportsClarificationTool ?? false,
    supportsPluginManagerTool: overrides.supportsPluginManagerTool ?? false,
    supportsNativeDelegation: overrides.supportsNativeDelegation ?? true,
    pluginCatalog: overrides.pluginCatalog,
    installedSkills: overrides.installedSkills,
    pluginActions: overrides.pluginActions,
    dataSurface: overrides.dataSurface,
    ...(overrides.graphjinDataPath ? { graphjinDataPath: overrides.graphjinDataPath } : {}),
    ...(overrides.dataSurface === "records"
      ? {
          appContext: {
            appId: "crm",
            appLabel: "CRM",
          },
          recordContext: {
            surface: "list" as const,
            appId: "crm",
            appLabel: "CRM",
            objectApiName: "activity",
            objectLabel: "Activities",
          },
        }
      : {}),
    inlineTranscript: false,
  });
}

describe("buildWorkPrompt UX metadata", () => {
  it("omits only the UX closing instructions for efficacy evals", () => {
    const production = build("hermes");
    expect(production).toContain(VALUE_ESTIMATE_INSTRUCTIONS);
    const evaluation = build("hermes", { includeUxMetadata: false });
    for (const block of ["neko_value", "neko_vitals", "neko_followups"]) {
      expect(production).toContain(block);
      expect(evaluation).not.toContain(block);
    }
    expect(evaluation).toBe(production.replace(/\n\n<closing>[\s\S]*?<\/closing>/, ""));
  });
});

describe("buildWorkPrompt GraphJin agent path", () => {
  it("describes only GraphJin's agent for data, including workflow triggers", () => {
    const agent = build("hermes", { graphjinDataPath: "agent", supportsWorkflowTool: true });
    expect(agent).toContain("`mcp_neko_graphjin_agent_ask`");
    expect(agent).not.toMatch(/mcp_neko_graphjin_(execute_graphql|query_catalog|validate_where_clause)/);
    expect(agent).not.toContain(knowledge.syntax);
    expect(agent).toContain("- Knowledge:");
    expect(agent).toContain("Write each instruction as a precise data request.");
    expect(agent).not.toMatch(/hidden|not available/i);
  });

  it("keeps the direct path unchanged", () => {
    const direct = build("hermes", { supportsWorkflowTool: true });
    expect(direct).toBe(build("hermes", { graphjinDataPath: "direct", supportsWorkflowTool: true }));
    expect(direct).toContain("`mcp_neko_graphjin_execute_graphql`");
    expect(direct).toContain("`mcp_neko_graphjin_query_catalog`");
    expect(direct).not.toContain("mcp_neko_graphjin_agent_ask");
  });
});

describe("buildWorkPrompt records data surface", () => {
  it("routes generated-app questions only to native records tools", () => {
    const prompt = build("hermes", {
      dataSurface: "records",
      supportsSourceConfigTool: true,
    });
    expect(prompt).toContain("<records_access>");
    expect(prompt).toContain("mcp_neko_records_*");
    expect(prompt).toContain('"appId":"crm"');
    expect(prompt).toContain('"objectApiName":"activity"');
    expect(prompt).toContain("default subject and conversational home");
    expect(prompt).toContain("CRM account context plus Support tickets");
    expect(prompt).toContain("existing app, object, and field");
    expect(prompt).not.toContain("<data_access>");
    expect(prompt).not.toContain("graphjin cli execute_graphql");
    expect(prompt).not.toContain("neko_source_config_manager");
  });
});

describe("buildWorkPrompt attachments guidance", () => {
  it("tells Hermes how to read uploads and which path shape to expect", () => {
    const prompt = build("hermes");
    expect(prompt).toContain("<attachments>");
    expect(prompt).toContain("uploads/<threadId>/<filename>");
    expect(prompt).toContain("`Read` tool");
    // Path is relative to cwd, which is orgRoot.
    expect(prompt).toContain(workspace.orgRoot);
  });

  it("references the hermes shell tool when running under hermes", () => {
    const prompt = build("hermes");
    expect(prompt).toContain("<attachments>");
    // The attachments block names Hermes' terminal tool for non-text formats.
    expect(prompt).toContain("`terminal`");
  });

  it("no longer dismisses uploaded files as 'auxiliary'", () => {
    const prompt = build("hermes");
    // The old wording told the model uploaded files were auxiliary, which it
    // routinely took as permission to ignore them. The new framing must
    // explicitly say to read them.
    expect(prompt).not.toMatch(/Uploaded files are auxiliary/);
    expect(prompt).toMatch(/read the file/i);
  });
});

describe("buildWorkPrompt clarification contract", () => {
  it("tells an interactive agent to clarify material uncertainty and stop", () => {
    const prompt = build("hermes", { supportsClarificationTool: true });
    expect(prompt).toContain("<clarification>");
    expect(prompt).toContain("mcp_neko_interaction_ask_user_question");
    expect(prompt).toContain("FINAL action of the turn");
    expect(prompt).toContain("material to correctness, scope, cost");
    expect(prompt).toMatch(/state\s+the limitation and continue/);
    expect(prompt).toContain("never permits invented sources");
    expect(prompt).toContain("clarification-tool call is the end");
  });

  it("publishes only run artifacts as downloads", () => {
    const prompt = build("hermes", { supportsClarificationTool: true });
    expect(prompt).toContain("published as downloadable artifacts");
    expect(prompt).toContain("download boundary rejects every file");
    expect(prompt).toContain("uploads, skills, memory, knowledge");
  });
});

describe("buildWorkPrompt conversation contract", () => {
  it("keeps prose conversational without prompting the model for progress", () => {
    const prompt = build("hermes", {
      wantsCards: true,
      supportsCardTool: true,
    });
    expect(prompt).toContain("ongoing working conversation");
    expect(prompt).toContain("The cards hold the figures; your sentences interpret");
    expect(prompt).toContain("state the answer itself in\none or two plain sentences");
    expect(prompt).not.toMatch(/emit (?:an )?(?:interim|progress) update/i);
    expect(prompt).not.toMatch(/before (?:each|every) tool/i);
  });
});

describe("buildWorkPrompt skill catalog", () => {
  it("includes compact discovery metadata and a path for on-demand instructions", () => {
    const prompt = build("hermes", {
      installedSkills: [
        { name: "pdf", description: "Read, create, and inspect PDF files." },
      ],
    });

    expect(prompt).toContain("pdf — Read, create, and inspect PDF files.");
    expect(prompt).toContain(`${workspace.skillsRoot}/pdf/SKILL.md`);
    expect(prompt).toContain("When a skill matches, read its SKILL.md");
    expect(prompt).not.toContain("Full PDF skill body");
  });

  it("uses typed mention metadata to distinguish skills from workflows", () => {
    const prompt = build("hermes", {
      installedSkills: [
        { name: "revenue-review", description: "Inspect revenue movement." },
      ],
      supportsWorkflowTool: true,
    });

    expect(prompt).toContain("<selected_mentions>");
    expect(prompt).toContain("::neko-work-mentions::");
    expect(prompt).toContain('kind:\"skill\"');
    expect(prompt).toContain('kind:\"workflow\"');
    expect(prompt).toContain("explicit kind and id win");
    expect(prompt).toContain(`${workspace.skillsRoot}/<name>/SKILL.md`);
    expect(prompt).toContain("::neko-workflow-mentions::");
    expect(prompt).toContain("Never quote, summarize, or echo");
  });
});

describe("buildWorkPrompt action scopes", () => {
  it("teaches fence-based agents the fixed scope for records and plugin actions", () => {
    const prompt = build("hermes", {
      pluginActions: [
        {
          kind: "record_create",
          description: "Create a generated-app record.",
          scope: "internal",
          default_mode: "ask",
        },
        {
          kind: "send_slack_dm",
          description: "Send a Slack direct message.",
          default_mode: "ask",
        },
      ],
    });

    expect(prompt).toContain("`record_create` (scope:internal; mode:ask)");
    expect(prompt).toContain("`send_slack_dm` (scope:external; mode:ask)");
    expect(prompt).toContain(
      '"scope": "<the exact scope shown for the selected kind>"',
    );
    expect(prompt).toMatch(/Never relabel an internal records\s+action as external/);
  });
});

describe("per-channel rendering gate", () => {
  it("renders via the render_cards tool on web turns (wantsCards)", () => {
    const hermesMcp = build("hermes", { wantsCards: true, supportsCardTool: true });
    expect(hermesMcp).toContain("<cards>");
    expect(hermesMcp).toContain("mcp__neko__ui_render_cards");
    expect(hermesMcp).not.toContain("mcp_neko_ui_render_cards");
    expect(hermesMcp).toContain('Three or more time periods: a table of the periods with chart "line"');
    expect(hermesMcp).toContain('A breakdown by category: a table with chart "bar"');
    expect(hermesMcp).toContain('{"title": "Website sessions by month", "table": "Month | Sessions\\nMarch | 39800');
    expect(hermesMcp).toContain("add a watch callout");
    expect(hermesMcp).not.toMatch(/A2UI|createSurface|surfaceId|envelope/);
    expect(hermesMcp).not.toContain("BriefingCard");

    const hermesWeb = build("hermes", { wantsCards: true, supportsCardTool: false });
    expect(hermesWeb).toContain("<cards>");
    expect(hermesWeb).toContain("render_cards");
    // The web-UI-coupled fence is gone from the prompt entirely.
    expect(hermesWeb).not.toContain("neko_a2ui");
  });

  it("omits all rendering vocabulary on non-web turns", () => {
    const prompt = build("hermes", { wantsCards: false, supportsCardTool: true });
    expect(prompt).not.toContain("<cards>");
    expect(prompt).not.toContain("neko_a2ui");
    expect(prompt).not.toContain("render_cards");
  });
});

describe("tool-result grounding", () => {
  it("forbids unsupported figures and failed sources in prose, cards, and vitals", () => {
    const prompt = build("hermes", {
      wantsCards: true,
      supportsCardTool: true,
    });

    expect(prompt).toContain("A failed source is unavailable");
    expect(prompt).toContain("Never invent per-day, per-period, or per-entity values");
    expect(prompt).toContain("Preserve the source's actual granularity");
    expect(prompt).toContain("Two daily rows plus a five-day aggregate is not a seven-day forecast");
    expect(prompt).toContain("use an");
    expect(prompt).toContain("available fetch or detail action");
    expect(prompt).toContain("state the exact");
    expect(prompt).toContain("coverage obtained");
    expect(prompt).toContain("Every claim and figure in the cards comes from a successful tool result");
    expect(prompt).toContain("Report a failed tool call as an error.");
    expect(prompt).toContain("Omit unsupported numbers");
    expect(prompt).toContain("attribute each claim, figure, or row");
    expect(prompt).toContain("use a shared source label only when every named");
    expect(prompt).toContain("displayed rows must reconcile");
    expect(prompt).toContain("explicitly stated multi-day summaries");
    expect(prompt).toContain("use it instead of");
    expect(prompt).toContain("direct network access from the terminal");
  });
});

describe("buildWorkPrompt workflow + policy management", () => {
  it("advertises workflow tools when supportsWorkflowTool is true", () => {
    const prompt = build("hermes", { supportsWorkflowTool: true });
    expect(prompt).toContain("mcp_neko_workflow_builder_list_workflows");
    expect(prompt).toContain("mcp_neko_workflow_builder_create_workflow");
    // Operators are not developers — should warn against showing cron syntax.
    expect(prompt).toMatch(/never show them cron syntax/i);
  });

  it("falls back to the workflow save fence when MCP tools unavailable", () => {
    const prompt = build("hermes", { supportsWorkflowTool: false });
    expect(prompt).toContain("neko_workflow_save");
    expect(prompt).not.toContain("mcp_neko_workflow_builder_");
  });

  it("teaches the data-change trigger in both workflow tool modes", () => {
    const mcp = build("hermes", { supportsWorkflowTool: true });
    expect(mcp).toContain("triggers.when");
    expect(mcp).not.toContain("create_subscription");
    expect(mcp).not.toContain("dry_run");

    const fence = build("hermes", { supportsWorkflowTool: false });
    expect(fence).toContain("triggers.when");
    // The trigger must not surface as a separate "subscription" tool/fence.
    expect(fence).not.toContain("neko_subscription");
    expect(fence).not.toContain("create_subscription");
  });

  it("advertises rule tools when supportsPolicyTool is true", () => {
    const prompt = build("hermes", { supportsPolicyTool: true });
    expect(prompt).toContain("mcp_neko_rule_builder_list_rules");
    expect(prompt).toContain("mcp_neko_rule_builder_save_rule");
  });

  it("falls back to the rule save fence when MCP tools unavailable", () => {
    const prompt = build("hermes", { supportsPolicyTool: false });
    expect(prompt).toContain("neko_rule_save");
    expect(prompt).not.toContain("mcp_neko_rule_builder_");
  });

  it("advertises GraphJin source-config tools only when enabled", () => {
    const enabled = build("hermes", { supportsSourceConfigTool: true });
    expect(enabled).toContain("graphjin-config");
    expect(enabled).toContain(`${workspace.skillsRoot}/graphjin-config/SKILL.md`);
    expect(enabled).toContain("mcp_neko_source_config_manager_describe_source_graph");
    expect(enabled).toContain("Call `ask_graphjin_config_agent`");
    expect(enabled).toContain("Success for a view or explanation");
    expect(enabled).toContain("source_config_admin");
    expect(enabled).toContain("call `present_source_form` with the values you already know");
    expect(enabled).toContain("mcp_neko_source_config_manager_present_source_form");
    expect(enabled).toContain("imported OpenAPI asset ID");
    expect(enabled).toContain("managed local-file manifest or object-store bucket");
    expect(enabled).not.toContain("trusted host");
    expect(enabled).not.toContain("globally read-only");
    expect(enabled).not.toContain("outside the sandbox");

    const disabled = build("hermes", { supportsSourceConfigTool: false });
    expect(disabled).not.toContain("mcp_neko_source_config_manager_");
  });

  it("frames /work as the single chat surface for everything", () => {
    const prompt = build("hermes");
    expect(prompt).toMatch(/only chat surface/i);
  });
});

describe("buildWorkPrompt capability recovery", () => {
  it("uses the plugin manager approval tool when MCP tools are available", () => {
    const prompt = build("hermes", {
      supportsPluginManagerTool: true,
    });

    expect(prompt).toContain("mcp_neko_plugin_manager_list_plugins");
    expect(prompt).toContain(
      "mcp_neko_plugin_manager_request_plugin_install",
    );
    expect(prompt).toMatch(/approval request.*inline/i);
    expect(prompt).toMatch(/same answer/i);
    expect(prompt).toMatch(/operator's yes\/no question/i);
  });

  it("gives Hermes exact marketplace names and an approval-gated action fence", () => {
    const prompt = build("hermes", {
      pluginCatalog: {
        installed: [],
        available: [
          {
            name: "@openneko/weather",
            title: "Weather",
            description: "Live weather data.",
            version: "1.0.0",
          },
        ],
      },
    });

    expect(prompt).toContain("@openneko/weather");
    expect(prompt).toContain('"kind": "plugin_install"');
    expect(prompt).toContain('"risk_level": "high"');
    expect(prompt).toMatch(/does not install silently/i);
    expect(prompt).toMatch(/same answer/i);
    expect(prompt).toMatch(/operator's yes\/no question/i);
  });
});

describe("buildWorkPrompt native delegation guidance", () => {
  it("teaches Hermes to use delegate_task without OpenNeko-named profiles", () => {
    const prompt = build("hermes");
    expect(prompt).toContain("<delegation>");
    expect(prompt).toContain("delegate_task");
    expect(prompt).toContain("OpenNeko does not provide named subagent profiles");
    expect(prompt).not.toContain("researcher");
    expect(prompt).not.toContain("coder");
  });

  it("gives Ax its worker child instead of delegate_task", () => {
    const prompt = build("ax");
    expect(prompt).toContain("<delegation>");
    expect(prompt).toContain("team.worker({task})");
    expect(prompt).not.toContain("delegate_task");
  });

  it("omits native delegation guidance when the run disables it", () => {
    const prompt = build("hermes", { supportsNativeDelegation: false });
    expect(prompt).not.toContain("<delegation>");
    expect(prompt).not.toContain("delegate_task");
  });
});

describe("buildBotSection", () => {
  it("names the bot and follows its own instructions", () => {
    const section = buildBotSection({ name: "Stock watcher", instructions: "Watches stock levels." }, true);
    expect(section).toContain('"Stock watcher"');
    expect(section).toContain("Watches stock levels.");
    expect(section).toContain("mcp_neko_bot_update_instructions");
  });

  it("describes the instructions tool only when it is mounted", () => {
    const section = buildBotSection({ name: "Test", instructions: "" }, false);
    expect(section).toContain("no job description yet");
    expect(section).not.toContain("mcp_neko_bot_");
  });

  it("leaves the prompt without a bot section for an ordinary chat", () => {
    expect(build("hermes")).not.toContain("<bot>");
  });
});

describe("buildWorkPrompt skills catalog", () => {
  const installedSkills = [
    { name: "xlsx", description: "Use this skill any time a spreadsheet file is the primary input or output" },
    { name: "docx", description: "Create and edit Word documents" },
  ];

  it("lists installed skills for Hermes", () => {
    const prompt = build("hermes", { installedSkills });
    expect(prompt).toContain("- xlsx — Use this skill any time a spreadsheet");
    expect(prompt).not.toContain("discover(");
  });

  it("sends Ax to skill search instead of listing every skill", () => {
    const prompt = build("ax", { installedSkills });
    expect(prompt).toContain("discover({ skills:");
    expect(prompt).not.toContain("- xlsx —");
    expect(prompt).not.toContain("Create and edit Word documents");
  });
});
