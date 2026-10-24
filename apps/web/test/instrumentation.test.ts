import { afterEach, expect, it, vi } from 'vitest';
const prepare = vi.hoisted(() => vi.fn());
vi.mock('@neko/llm/work/sandbox-launcher', () => ({ prepareSandboxCapacity: prepare }));
import { register } from '../src/instrumentation';
afterEach(() => { vi.unstubAllEnvs(); vi.restoreAllMocks(); prepare.mockReset(); });
it('starts the sandbox launcher while registering the Node server', async () => {
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', 'agent:test');
  await register();
  expect(prepare).toHaveBeenCalledTimes(1);
  expect(prepare).toHaveBeenCalledWith();
});
it('keeps existing pages available on preparation failure and skips unconfigured runtimes', async () => {
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', 'agent:test');
  vi.spyOn(console, 'error').mockImplementation(() => {});
  prepare.mockImplementation(() => { throw new Error('Invalid agent sandbox CPU limit'); });
  await expect(register()).resolves.toBeUndefined();
  vi.stubEnv('NEXT_RUNTIME', 'edge'); await register();
  vi.stubEnv('NEXT_RUNTIME', 'nodejs'); vi.stubEnv('OPENNEKO_AGENT_IMAGE', ''); await register();
  expect(prepare).toHaveBeenCalledTimes(1);
});
