import React from "react";

const storageKey = "graphjin.operatorIdentity.v1";
const suggestionDismissedKey = "graphjin.operatorIdentity.suggestionDismissed.v1";
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
    // Remember the clear, or the server's suggestion is re-adopted on the next
    // page load and clearing looks like it did nothing.
    window.localStorage.setItem(suggestionDismissedKey, "1");
    cachedRaw = "";
    cachedIdentity = emptyIdentity;
    window.dispatchEvent(new CustomEvent(identityEvent));
  }
}

// adoptSuggestedIdentity takes the development identity the server offers in
// console bootstrap, so a zero-configuration console starts inside the owner
// scope that already has tasks and watches. It never overwrites an identity the
// operator set, and never returns after they have cleared one.
export function adoptSuggestedIdentity(suggested) {
  if (typeof window === "undefined" || !suggested?.user_id) {
    return false;
  }
  try {
    // Unlike the rest of this module, this runs on every page load rather than
    // from a user action, so a browser that refuses storage must degrade to
    // "no suggestion" instead of breaking the console.
    if (window.localStorage.getItem(storageKey) || window.localStorage.getItem(suggestionDismissedKey)) {
      return false;
    }
    writeOperatorIdentity({
      userId: suggested.user_id,
      role: suggested.role,
      accountId: suggested.account_id,
    });
    return true;
  } catch {
    return false;
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
