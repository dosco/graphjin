const RUNTIME_MESSAGE =
  "Setup could not finish because the agent became unavailable. Confirm `openneko status` shows serving, re-test the model in Admin → Settings → Agent, then try again.";

const TIMEOUT_MESSAGE =
  "The profile agent reached its setup time limit before finishing. Re-test the model in Admin → Settings → Agent, then try again; if it repeats, inspect `openneko logs` for the recorded agent activity.";

const PROVIDER_MESSAGE =
  "Setup could not finish because the model provider rejected or interrupted the request. Re-test the model in Admin → Settings → Agent, then try again.";

const MODEL_HANDOFF_MESSAGE =
  "Setup could not finish because the worker could not load the saved model configuration. Re-save or re-test the model in Admin → Settings → Agent, then try again; if it repeats, inspect `openneko logs` for the configuration handoff error.";

const DATA_MESSAGE =
  "Setup could not finish while reading the configured data source. Re-test the source in Admin → Settings → Data Sources, then try again.";

const GENERIC_MESSAGE =
  "Setup could not finish. Inspect `openneko logs` for the recorded cause, then try again.";

/** Convert the persisted internal job failure into stable, actionable copy.
 * Raw provider output can contain paths or request details, so it is used only
 * for classification and is never returned to the browser. */
export function profileBuildFailureMessage(error: string | null): string {
  if (!error) return GENERIC_MESSAGE;
  if (
    /turn exceeded|timed? out|timeout|max(?:imum)? iterations|iteration budget/i.test(
      error,
    )
  ) {
    return TIMEOUT_MESSAGE;
  }
  if (
    /openneko serve|serve socket|agent sandbox|agent runtime|builtin-skills|mcp-bridge|scandir/i.test(
      error,
    )
  ) {
    return RUNTIME_MESSAGE;
  }
  if (
    /no llm provider configured|ax backend has no model configured/i.test(
      error,
    )
  ) {
    return MODEL_HANDOFF_MESSAGE;
  }
  if (
    /provider|gemini http|anthropic http|rate.?limit|quota|api key|authentication/i.test(
      error,
    )
  ) {
    return PROVIDER_MESSAGE;
  }
  if (/graphjin|execute_graphql|data source|unauthorized.*table/i.test(error)) {
    return DATA_MESSAGE;
  }
  return GENERIC_MESSAGE;
}
