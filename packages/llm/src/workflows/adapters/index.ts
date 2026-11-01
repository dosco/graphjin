import { registerActionAdapter } from "../action-executor";
import { draftPatchAdapter, makeCreateIssueAdapter } from "./code";
import { webhookAdapter } from "./webhook";

let registered = false;

/**
 * Register every built-in action adapter with the executor registry.
 * Idempotent; safe to call multiple times. Tests can call this to bring
 * the same adapters into scope.
 */
export function registerBuiltinAdapters(): void {
  if (registered) return;
  registered = true;
  registerActionAdapter("code_draft_patch", draftPatchAdapter);
  registerActionAdapter("send_webhook", webhookAdapter);
  registerActionAdapter("code_create_issue", makeCreateIssueAdapter());
}

export { webhookAdapter, WebhookAdapterError } from "./webhook";
export {
  CodeActionError,
  draftPatchAdapter,
  makeCreateIssueAdapter,
} from "./code";
