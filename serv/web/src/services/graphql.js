const defaultEndpoint = import.meta.env.VITE_DEFAULT_ENDPOINT || "/api/v1/graphql";

export class RequestError extends Error {
  constructor(message, options = {}) {
    super(message);
    this.name = "RequestError";
    this.status = options.status || 0;
    this.kind = options.kind || "unknown";
    this.errors = options.errors || [];
    this.responseText = options.responseText || "";
  }
}

export const endpointPath = () => {
  const params = new URLSearchParams(window.location.search);
  const requested = params.get("endpoint");
  if (requested && requested.startsWith("/") && !requested.startsWith("//")) {
    return requested;
  }
  return defaultEndpoint;
};

export async function graphqlRequest(query, variables = {}) {
  const payload = await fetchJSON(endpointPath(), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ query, variables }),
  });

  if (payload.errors?.length && !payload.data) {
    const message = payload.errors.map((error) => error.message).join("; ");
    throw new RequestError(message, {
      kind: classifyGraphQLError(message),
      errors: payload.errors,
    });
  }
  return payload;
}

export async function fetchJSON(url, options = {}) {
  let response;
  try {
    response = await fetch(url, {
      credentials: "same-origin",
      ...options,
      headers: {
        Accept: "application/json",
        ...(options.headers || {}),
      },
    });
  } catch (error) {
    throw new RequestError(`Service unavailable: ${error.message}`, {
      kind: "unavailable",
    });
  }

  const text = await response.text();
  let payload = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch (error) {
      throw new RequestError(`GraphJin returned a non-JSON response: ${error.message}`, {
        status: response.status,
        kind: response.ok ? "invalid" : classifyHTTPStatus(response.status),
        responseText: text.slice(0, 240),
      });
    }
  }

  if (!response.ok) {
    throw new RequestError(payload?.errors?.[0]?.message || payload?.message || `Request failed (${response.status})`, {
      status: response.status,
      kind: classifyHTTPStatus(response.status),
      errors: payload?.errors || [],
    });
  }
  return payload;
}

export function errorText(error, fallback = "This data is unavailable for the current role.") {
  if (!error) {
    return fallback;
  }
  return error.message || fallback;
}

export function classifyHTTPStatus(status) {
  if (status === 401 || status === 403) {
    return "auth";
  }
  if (status === 0 || status === 502 || status === 503 || status === 504 || status >= 500) {
    return "unavailable";
  }
  return "http";
}

function classifyGraphQLError(message) {
  if (/\b(unauthorized|forbidden|permission|blocked for role|access denied)\b/i.test(message || "")) {
    return "auth";
  }
  return "graphql";
}

export function graphQLErrors(payload) {
  return payload?.errors?.map((error) => error.message).filter(Boolean) || [];
}

export function parseJSON(value, fallback = null) {
  if (value == null || value === "") {
    return fallback;
  }
  if (typeof value !== "string") {
    return value;
  }
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

export function compactNumber(value) {
  const number = Number(value || 0);
  return new Intl.NumberFormat(undefined, { notation: number >= 10000 ? "compact" : "standard" }).format(number);
}

export function relativeTime(value) {
  if (!value) {
    return "just now";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const divisions = [
    { amount: 60, unit: "second" },
    { amount: 60, unit: "minute" },
    { amount: 24, unit: "hour" },
    { amount: 7, unit: "day" },
    { amount: 4.345, unit: "week" },
    { amount: 12, unit: "month" },
    { amount: Number.POSITIVE_INFINITY, unit: "year" },
  ];
  let duration = seconds;
  for (const division of divisions) {
    if (Math.abs(duration) < division.amount) {
      return new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(Math.round(duration), division.unit);
    }
    duration /= division.amount;
  }
  return date.toLocaleString();
}
