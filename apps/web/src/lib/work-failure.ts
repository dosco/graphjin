export type WorkFailurePresentation = {
  summary: string;
  detail: string;
  technical: boolean;
};

function httpStatus(message: string): number | null {
  if (!message.startsWith("HTTP ")) return null;
  const separator = message.indexOf(":", 5);
  const statusText = message.slice(5, separator === -1 ? undefined : separator).trim();
  const status = Number(statusText);
  return Number.isInteger(status) && status >= 400 && status <= 599
    ? status
    : null;
}

function isHtmlResponse(message: string): boolean {
  const lower = message.toLowerCase();
  return (
    lower.includes("<!doctype html") ||
    lower.includes("<html") ||
    lower.includes("<head") ||
    lower.includes("<body")
  );
}

/**
 * Converts agent/runtime failures into operator-facing copy. Technical bodies
 * remain available behind a Disclosure, but never become the primary answer.
 */
export function presentWorkFailure(message: string): WorkFailurePresentation {
  const detail = message.trim() || "No technical detail was provided.";
  const lower = detail.toLowerCase();
  const status = httpStatus(detail);
  const html = isHtmlResponse(detail);

  if (
    lower.includes("valid api key") ||
    lower.includes("invalid api key") ||
    lower.includes("authentication failed") ||
    lower.includes("unauthorized")
  ) {
    return {
      summary:
        "The model provider rejected its credentials. Check the provider settings, then retry.",
      detail,
      technical: true,
    };
  }

  if (
    lower.includes("package is required for the anthropic provider")
  ) {
    return {
      summary:
        "The selected model provider is unavailable in this OpenNeko installation. Update the agent runtime, then retry.",
      detail,
      technical: true,
    };
  }

  if (status === 404) {
    return {
      summary:
        "The selected model is not available from this provider. Check the model name and account access, then retry.",
      detail,
      technical: true,
    };
  }

  if (status === 429) {
    return {
      summary:
        "The model provider is temporarily limiting requests. Wait a moment, then retry.",
      detail,
      technical: true,
    };
  }

  if (html) {
    return {
      summary:
        "OpenNeko could not load this result. Retry it; if the problem continues, check the server logs.",
      detail:
        "The server returned an HTML error response. Its contents are omitted from the conversation.",
      technical: true,
    };
  }

  if (status !== null || lower.startsWith("internal error:")) {
    return {
      summary:
        "OpenNeko could not complete this run. Retry it; if the problem continues, check the agent setup.",
      detail,
      technical: true,
    };
  }

  return {
    summary:
      "OpenNeko could not complete this run. Retry it; if the problem continues, check the technical details.",
    detail,
    technical: false,
  };
}
