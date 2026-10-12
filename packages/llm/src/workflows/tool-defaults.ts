// Historical prompt/tool catalog constants retained for eval compatibility.
export const WORKFLOW_BUILDER_ALLOWED_TOOLS = [
  "Read",
  "Glob",
  "Grep",
  "WebFetch",
  "WebSearch",
  "AskUserQuestion",
  "mcp_neko_workflow_builder_create_workflow",
  "mcp_neko_workflow_builder_list_workflows",
] as const;

export const WORKFLOW_RUNNER_DEFAULT_ALLOWED_TOOLS = [
  "Bash",
  "Read",
  "Write",
  "Edit",
  "Glob",
  "Grep",
  "WebFetch",
  "WebSearch",
  "AskUserQuestion",
  "Agent",
  "Skill",
  "mcp_neko_ui_render_cards",
  "mcp_neko_memory_search",
  "mcp_neko_memory_save",
  "mcp_neko_memory_update",
  "mcp_neko_library_search",
  "mcp_neko_workflow_output_emit",
  "mcp_neko_action_request",
] as const;
