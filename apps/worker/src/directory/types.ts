/**
 * Directory contract. A directory provider (Scalekit today) supplies the
 * users, groups and group memberships of the connected identity provider.
 * OpenNeko mirrors them and applies its IdP group rules; the provider
 * decides memberships, OpenNeko decides what each group may use.
 */

export interface DirectoryUser {
  /** Stable provider user id, used only to join memberships in one snapshot. */
  externalId: string;
  /** OIDC subject when the provider knows it; sign-in matches on it. */
  sub?: string | null;
  email: string;
  name?: string | null;
  active: boolean;
}

export interface DirectoryGroup {
  /** Immutable provider group id; the same id sign-in claims carry. */
  externalId: string;
  name?: string | null;
}

export interface DirectoryMembership {
  userExternalId: string;
  groupExternalId: string;
}

export interface ListDirectoryParams {
  cursor?: string | null;
}

export interface ListDirectoryResult {
  /** Provider tenant, e.g. the Scalekit organization id. Must match sign-in `orgId`. */
  tenantId: string;
  users: DirectoryUser[];
  groups: DirectoryGroup[];
  memberships: DirectoryMembership[];
  /** Present when more pages follow. */
  nextCursor?: string | null;
}

export type DirectoryChange =
  | { op: "create_user"; email: string; name?: string | null }
  | { op: "deactivate_user"; externalId: string };

export interface ApplyDirectoryChangeParams {
  change: DirectoryChange;
}

export interface ApplyDirectoryChangeResult {
  user?: DirectoryUser | null;
}

/** What a directory provider reads, and which changes it accepts. */
export interface DirectoryCapabilities {
  providerLabel: string;
  read: { users: boolean; groups: boolean; memberships: boolean };
  write: { createUser: boolean; deactivateUser: boolean };
}

export function directoryChangeAllowed(capabilities: DirectoryCapabilities, change: DirectoryChange): boolean {
  return change.op === "create_user" ? capabilities.write.createUser : capabilities.write.deactivateUser;
}
