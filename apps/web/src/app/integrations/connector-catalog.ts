/** Connectors OpenNeko ships as packs. Each shows in Connectors before an admin sets it up. */
export const SUPPORTED_CONNECTORS = [
  {
    packId: "google-workspace",
    label: "Google Workspace",
    icon: "/connectors/google-workspace.svg",
    description: "Connect Gmail, Drive, Calendar, Sheets, Docs, and Slides for your work and automations.",
  },
] as const;

export const COMING_SOON_CONNECTORS = [
  { label: "Microsoft Teams", icon: "/connectors/microsoft-teams.svg", description: "Chats, channels, and meetings." },
  { label: "Atlassian", icon: "/connectors/atlassian.svg", description: "Jira issues and Confluence pages." },
] as const;

/** The brand mark for a pack's connector, when OpenNeko ships one. */
export function connectorIcon(packId: string): string | null {
  return SUPPORTED_CONNECTORS.find((connector) => connector.packId === packId)?.icon ?? null;
}
