import React from "react";

const storageKey = "graphjin.operatorIdentity.v1";
const identityEvent = "graphjin:operator-identity";
const emptyIdentity = Object.freeze({ userId: "", role: "", accountId: "" });
let cachedRaw = null;
let cachedIdentity = emptyIdentity;

export function readOperatorIdentity() {
  if (typeof window === "undefined") {
    return emptyIdentity;
  }
  const raw = window.localStorage.getItem(storageKey) || "";
  if (raw === cachedRaw) {
    return cachedIdentity;
  }
  try {
    cachedRaw = raw;
    cachedIdentity = normalizeIdentity(JSON.parse(raw || "{}"));
  } catch {
    cachedRaw = raw;
    cachedIdentity = emptyIdentity;
  }
  return cachedIdentity;
}

export function writeOperatorIdentity(identity) {
  const normalized = normalizeIdentity(identity);
  if (typeof window !== "undefined") {
    const raw = JSON.stringify(normalized);
    window.localStorage.setItem(storageKey, raw);
    cachedRaw = raw;
    cachedIdentity = normalized;
    window.dispatchEvent(new CustomEvent(identityEvent));
  }
  return normalized;
}

export function clearOperatorIdentity() {
  if (typeof window !== "undefined") {
    window.localStorage.removeItem(storageKey);
    cachedRaw = "";
    cachedIdentity = emptyIdentity;
    window.dispatchEvent(new CustomEvent(identityEvent));
  }
}

export function operatorIdentityHeaders(identity = readOperatorIdentity()) {
  if (!identity?.userId) {
    return {};
  }
  return {
    "X-User-ID": identity.userId,
    ...(identity.role ? { "X-User-Role": identity.role } : {}),
    ...(identity.accountId ? { "X-Account-ID": identity.accountId } : {}),
  };
}

export function operatorIdentityKey(identity = readOperatorIdentity()) {
  return [identity?.userId || "", identity?.role || "", identity?.accountId || ""].join(":");
}

export function hasOperatorIdentity(identity) {
  return Boolean(identity?.userId);
}

export function useOperatorIdentity() {
  return React.useSyncExternalStore(subscribe, readOperatorIdentity, () => emptyIdentity);
}

function subscribe(onStoreChange) {
  if (typeof window === "undefined") {
    return () => {};
  }
  const handleStorage = (event) => {
    if (!event.key || event.key === storageKey) {
      onStoreChange();
    }
  };
  window.addEventListener(identityEvent, onStoreChange);
  window.addEventListener("storage", handleStorage);
  return () => {
    window.removeEventListener(identityEvent, onStoreChange);
    window.removeEventListener("storage", handleStorage);
  };
}

function normalizeIdentity(identity = {}) {
  return {
    userId: String(identity.userId || "").trim(),
    role: String(identity.role || "").trim(),
    accountId: String(identity.accountId || "").trim(),
  };
}
