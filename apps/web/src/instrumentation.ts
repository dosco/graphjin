/** Start the sweep of stranded agent runs before serving requests. */
export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  process.env.OPENNEKO_SANDBOX_OWNER ||= "web";
  if (!process.env.OPENNEKO_SERVE_SOCKET) return;
  const { prepareSandboxCapacity } = await import("@neko/llm/work/sandbox-launcher");
  try {
    await prepareSandboxCapacity();
  } catch (error) {
    // Keep setup and existing pages available if the sweep fails; a run
    // still stages and removes its own folder.
    console.error("[sandbox] startup preparation failed", error);
  }
}
