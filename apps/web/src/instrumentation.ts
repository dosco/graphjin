/** Start the sandbox launcher before serving requests. */
export async function register() {
  if (process.env.NEXT_RUNTIME !== "nodejs") return;
  process.env.OPENNEKO_SANDBOX_OWNER ||= "web";
  if (!process.env.OPENNEKO_AGENT_IMAGE) return;
  const { prepareSandboxCapacity } = await import("@neko/llm/work/sandbox-launcher");
  try {
    await prepareSandboxCapacity();
  } catch (error) {
    // Keep setup and existing pages available during a gateway outage. Work
    // admission still waits for a ready sandbox.
    console.error("[sandbox] startup preparation failed", error);
  }
}
