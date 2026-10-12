import { afterEach, expect, it, vi } from 'vitest';
const prepare = vi.hoisted(() => vi.fn());
vi.mock('@neko/llm/work/sandbox-launcher', () => ({ prepareSandboxCapacity: prepare }));
vi.mock('@neko/db', () => ({ getOrgId: async () => 'org-test' }));
vi.mock('@neko/llm/work', () => ({ ensureOrgWorkspace: async (orgId: string) => ({ orgRoot: '/orgs/' + orgId }) }));
vi.mock('@neko/llm', () => ({ resolveAgentBackendId: async () => 'hermes' }));
import { register } from '../src/instrumentation';
afterEach(() => { vi.unstubAllEnvs(); vi.restoreAllMocks(); prepare.mockReset(); });
it('waits for warm readiness before registering the Node server', async () => {
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', 'agent:test');
  let ready!: () => void;
  prepare.mockImplementation(() => new Promise<void>(resolve => { ready = resolve; }));
  let registered = false;
  const registration = register().then(() => { registered = true; });
  await vi.waitFor(() => expect(prepare).toHaveBeenCalledTimes(1));
  expect(registered).toBe(false);
  expect(prepare).toHaveBeenCalledWith(undefined, { orgRoot: "/orgs/org-test" }, 'hermes');
  ready(); await registration; expect(registered).toBe(true);
});
it('keeps existing pages available on preparation failure and skips unconfigured runtimes', async () => {
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', 'agent:test');
  vi.spyOn(console, 'error').mockImplementation(() => {});
  prepare.mockRejectedValue(new Error('gateway offline'));
  await expect(register()).resolves.toBeUndefined();
  vi.stubEnv('NEXT_RUNTIME', 'edge'); await register();
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', ''); await register();
  expect(prepare).toHaveBeenCalledTimes(1);
});
