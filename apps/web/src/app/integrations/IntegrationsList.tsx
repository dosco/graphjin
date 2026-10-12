"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { Plug } from "lucide-react";
import type { PackUserConnectionStatus } from "@neko/llm/graphjin/pack-user-connections";
import { confirmDialog } from "@/components/ConfirmModal";
import { ActionGroup } from "@/components/ui/action-group";
import { Card, CardHeader, CardContent, CardFooter } from "@/components/ui/card";
import { OverflowMenu, MenuItem } from "@/components/ui/overflow-menu";
import { Disclosure } from "@/components/ui/disclosure";
import { Badge } from "@/components/ui/badge";
import { LocalDateTime } from "@/components/ui/local-date-time";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import { Button, ButtonLink } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { SearchInput } from "@/components/ui/search-input";
import { matchesListSearch } from "@/lib/list-search";
import Image from "next/image";
import { COMING_SOON_CONNECTORS, SUPPORTED_CONNECTORS, connectorIcon } from "./connector-catalog";

type Row = {
  pluginId: string;
  pluginName: string;
  providerLabel: string;
  scopes: string[];
  flow: string;
  credentialScope: string;
  connected: boolean;
  connectedAt: string | null;
};

type InitialState = { workspace: Row[]; connectors: Row[]; personal?: PackUserConnectionStatus[] };

function ConnectorTitle({ label, icon }: { label: string; icon: string | null }) {
  return (
    <span className="flex min-w-0 items-center gap-3">
      {icon ? <Image src={icon} alt="" width={28} height={28} className="size-7 shrink-0 object-contain" /> : null}
      <h3 className="font-display text-ui-subsection font-bold text-text break-words">{label}</h3>
    </span>
  );
}

export default function IntegrationsList({ initial, isAdmin = true, preview = false, variant = "page", canSetUp = false }: { initial: InitialState; isAdmin?: boolean; preview?: boolean; variant?: "page" | "chat"; canSetUp?: boolean }) {
  const returnQuery = variant === "chat" ? "?returnTo=/chat/connectors" : "";
  const [personal, setPersonal] = useState(initial.personal ?? []);
  const [workspace, setWorkspace] = useState<Row[]>(initial.workspace);
  const [connectors, setConnectors] = useState<Row[]>(initial.connectors);
  const [busy, setBusy] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const params = useSearchParams();
  const visiblePersonal = personal.filter(row => matchesListSearch(query, row.providerLabel, row.packId, row.accountLabel ?? "", ...row.scopes));
  const visibleWorkspace = workspace.filter((row) =>
    matchesListSearch(
      query,
      row.pluginId,
      row.pluginName,
      row.providerLabel,
      row.flow,
      row.credentialScope,
      ...row.scopes,
    ),
  );
  const visibleConnectors = connectors.filter((row) =>
    matchesListSearch(
      query,
      row.pluginId,
      row.pluginName,
      row.providerLabel,
      row.flow,
      row.credentialScope,
      ...row.scopes,
    ),
  );

  useEffect(() => {
    const err = params.get("error");
    if (err) toast.error(err);
    const ok = params.get("connected");
    if (ok) toast.success(`Connected ${ok}`);
  }, [params]);

  async function disconnect(row: Row, isDeployment: boolean) {
    if (preview || busy !== null || (isDeployment && !isAdmin)) return;
    if (!await confirmDialog({
      title: `Disconnect ${row.providerLabel}?`,
      description: isDeployment
        ? "This removes the shared connection for everyone in the workspace. Automations using it will need the connection restored."
        : "Your automations will lose access to this account until you reconnect it.",
      confirmLabel: "Disconnect",
      destructive: true,
    })) return;
    const { pluginName } = row;
    setBusy(pluginName);
    try {
      const res = await fetch(
        `/api/integrations/disconnect/${encodeURIComponent(pluginName)}`,
        { method: "POST" },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      const update = (prev: Row[]) =>
        prev.map((r) =>
          r.pluginName === pluginName
            ? { ...r, connected: false, connectedAt: null }
            : r,
        );
      if (isDeployment) setWorkspace(update);
      else setConnectors(update);
      toast.success(`Disconnected ${row.providerLabel}`);
    } catch (e) {
      toast.error(`Could not disconnect ${row.providerLabel}. Try again.`, {
        description: e instanceof Error ? e.message : String(e),
      });
    } finally {
      setBusy(null);
    }
  }

  async function managePersonal(row: PackUserConnectionStatus, disconnect = false) {
    if (preview) return;
    if (disconnect && !await confirmDialog({ title: `Disconnect ${row.providerLabel}?`, description: "Your automations will lose access. Pending approvals using this connection will need to be requested again.", confirmLabel: "Disconnect", destructive: true })) return;
    setBusy(`${row.packId}:${row.key}`);
    try {
      const response = await fetch(`/api/my/pack-accounts/${encodeURIComponent(row.packId)}/${encodeURIComponent(row.key)}${returnQuery}`, { method: disconnect ? "DELETE" : "POST" });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error ?? "Connection failed");
      if (!disconnect) { window.location.assign(result.authorizationUrl); return; }
      setPersonal(rows => rows.map(value => value.packId === row.packId && value.key === row.key ? { ...value, connected: false, accountLabel: null } : value));
      toast.success(`Disconnected ${row.providerLabel}`);
    } catch (error) { toast.error(error instanceof Error ? error.message : "Connection failed"); }
    finally { setBusy(null); }
  }

  async function explainSetup(label: string) {
    await confirmDialog({
      title: `${label} needs setup`,
      description: `An admin sets up ${label} for your workspace first. Ask an admin to install and configure it, then connect your account here.`,
      confirmLabel: "OK",
      cancelLabel: "Close",
    });
  }

  const notSetUp = variant === "chat"
    ? SUPPORTED_CONNECTORS.filter((connector) => !personal.some((row) => row.packId === connector.packId))
    : [];

  function renderPluginConnection(row: Row, isDeployment: boolean) {
    const disabled = preview || busy !== null || (isDeployment && !isAdmin);
    return (
      <Card as="li"
        key={row.pluginName}
        className="grid min-w-0 gap-4"
        aria-busy={busy === row.pluginName}
      >
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="font-display text-ui-subsection font-bold text-text break-words">{row.providerLabel}</h3>
            <Badge variant={row.connected ? "success" : "muted"}>{row.connected ? "Connected" : "Not connected"}</Badge>
          </div>
          {row.connected && row.connectedAt && (
            <p className="text-ui-body-sm text-text2">
              Connected <LocalDateTime value={row.connectedAt} fallback="recently" />
            </p>
          )}
          {!row.connected && <p className="text-ui-body-sm text-text2">{isDeployment && !isAdmin ? "An admin can connect this workspace account." : "Connect your account to get started."}</p>}
        </CardHeader>
        <CardFooter>
          <ActionGroup align="start">
            {isAdmin && !preview && "pluginName" in row ? (
              <ButtonLink href={`/admin/access?type=integration&id=${encodeURIComponent(String(row.pluginName))}`}>Access</ButtonLink>
            ) : null}
            {row.connected ? (
              <Button variant="danger" disabled={disabled} onClick={() => void disconnect(row, isDeployment)}>
                {busy === row.pluginName ? "Disconnecting…" : "Disconnect"}
              </Button>
            ) : disabled ? <Button disabled>Connect account</Button> : (
              <ButtonLink variant="primary" href={`/api/integrations/connect/${encodeURIComponent(row.pluginName)}/start${returnQuery}`}>Connect account</ButtonLink>
            )}
          </ActionGroup>
        </CardFooter>
        <Disclosure title="Permissions and details">
          <div className="grid min-w-0 gap-3 text-ui-caption text-text2">
            <p className="break-all">Plugin: {row.pluginName}</p>
            {row.scopes.length > 0 ? <ul className="break-all">{row.scopes.map(scope => <li key={scope}>{scope}</li>)}</ul> : <p>No additional permissions requested.</p>}
          </div>
        </Disclosure>
      </Card>
    );
  }

  const Wrapper = variant === "chat" ? "main" : "div";
  return (
    <Wrapper className={variant === "chat" ? "chat-connectors" : "root is-narrow"}>
      {variant === "chat" ? (
        <div className="chat-home-head">
          <div>
            <h1 className="chat-home-title">Connectors</h1>
            <p className="chat-connectors-copy">Connect your own accounts. Your bots can use everything you connect.</p>
          </div>
        </div>
      ) : (
        <>
          <AppHeader />
          <PageHeading
            title="Integrations"
            description="Connect external accounts for agent actions. Credentials remain in this deployment."
          />
        </>
      )}

      {preview && <p role="status" className="mt-4 text-ui-body-sm text-text2">Visual preview. Account actions are disabled.</p>}

      {workspace.length + connectors.length + personal.length > 0 ? (
        <div className="mt-6 max-w-[520px]">
          <SearchInput
            label="Search integrations"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search integrations and scopes"
          />
        </div>
      ) : null}

      {visiblePersonal.length > 0 && <section aria-labelledby="my-connections">
        <h2 id="my-connections" className="mt-6 mb-2 font-display text-ui-section font-bold text-text">My connections</h2>
        <p className="text-ui-body-sm text-text3 mb-3">Connect your own accounts for your work and automations. Other users connect separately.</p>
        <ul className="flex flex-col gap-3">{visiblePersonal.map(row => <Card as="li" key={`${row.packId}:${row.key}`} className="grid gap-4" aria-busy={busy === `${row.packId}:${row.key}`}>
          <CardHeader>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <ConnectorTitle label={row.providerLabel} icon={connectorIcon(row.packId)} />
              <Badge variant={row.connected ? "success" : "muted"}>{row.connected ? "Connected" : "Not connected"}</Badge>
            </div>
            <p className="text-ui-body-sm text-text2 break-words">{row.accountLabel ?? (row.configured ? "Connect your account to get started." : "An admin needs to finish setup first.")}</p>
          </CardHeader>
          {row.experience?.description && <CardContent><p className="text-ui-body-sm text-text2">{row.experience.description}</p></CardContent>}
          <CardFooter>
            <ActionGroup align="start">
              <Button size="md" variant={row.connected ? "secondary" : "primary"} disabled={preview || busy !== null || !row.configured} onClick={() => void managePersonal(row)}>{busy === `${row.packId}:${row.key}` ? "Working…" : row.connected ? "Reconnect" : "Connect account"}</Button>
              {row.connected && <OverflowMenu size="icon" label={`More actions for ${row.providerLabel}`} align="start"><MenuItem danger disabled={preview || busy !== null} onSelect={() => void managePersonal(row, true)}>Disconnect</MenuItem></OverflowMenu>}
            </ActionGroup>
          </CardFooter>
          <Disclosure title="Permissions and help"><div className="grid min-w-0 gap-4"><ul className="min-w-0 text-ui-caption text-text2 break-all">{row.scopes.map(scope => <li key={scope}>{scope}</li>)}</ul>{row.experience?.helpUrl && <ButtonLink size="md" className="justify-self-start" href={row.experience.helpUrl} target="_blank" rel="noreferrer">Connection help</ButtonLink>}</div></Disclosure>
        </Card>)}</ul>
      </section>}

      {notSetUp.length > 0 && (
        <section aria-labelledby="available-connectors">
          <h2 id="available-connectors" className="mt-6 mb-2 font-display text-ui-section font-bold text-text">Available</h2>
          <ul className="flex flex-col gap-3">
            {notSetUp.map((connector) => (
              <Card as="li" key={connector.packId} className="grid gap-4">
                <CardHeader>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <ConnectorTitle label={connector.label} icon={connector.icon} />
                    <Badge variant="muted">Needs setup</Badge>
                  </div>
                  <p className="text-ui-body-sm text-text2">{connector.description}</p>
                </CardHeader>
                <CardFooter>
                  <ActionGroup align="start">
                    {canSetUp ? (
                      <ButtonLink href="/admin/settings/packs">Set up in Admin</ButtonLink>
                    ) : (
                      <Button onClick={() => void explainSetup(connector.label)}>Connect account</Button>
                    )}
                  </ActionGroup>
                </CardFooter>
              </Card>
            ))}
          </ul>
        </section>
      )}

      {variant === "chat" && (
        <section aria-labelledby="coming-soon-connectors">
          <h2 id="coming-soon-connectors" className="mt-6 mb-2 font-display text-ui-section font-bold text-text">Coming soon</h2>
          <ul className="flex flex-col gap-3">
            {COMING_SOON_CONNECTORS.map((connector) => (
              <Card as="li" key={connector.label} className="grid gap-2">
                <CardHeader>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <ConnectorTitle label={connector.label} icon={connector.icon} />
                    <Badge variant="secondary">Coming soon</Badge>
                  </div>
                  <p className="text-ui-body-sm text-text2">{connector.description}</p>
                </CardHeader>
              </Card>
            ))}
          </ul>
        </section>
      )}

      {visibleWorkspace.length > 0 && (
        <>
          <h2 className="mt-6 mb-2 font-display text-ui-section font-bold text-text">
            Workspace connections
          </h2>
          <p className="text-ui-body-sm text-text3 mb-2">
            An admin connects these accounts for everyone in the workspace.
            Review the requested permissions before connecting.
          </p>
          <ul className="flex flex-col gap-3">
            {visibleWorkspace.map((row) => (
              renderPluginConnection(row, true)
            ))}
          </ul>
        </>
      )}

      {visibleConnectors.length > 0 && (
        <>
          <h2 className="mt-6 mb-2 font-display text-ui-section font-bold text-text">
            Personal plugin connections
          </h2>
          <p className="text-ui-body-sm text-text3 mb-2">
            These plugin accounts are connected only for you. Other users connect separately.
          </p>
          <ul className="flex flex-col gap-3">
            {visibleConnectors.map((row) => (
              renderPluginConnection(row, false)
            ))}
          </ul>
        </>
      )}

      {variant !== "chat" && workspace.length === 0 && connectors.length === 0 && personal.length === 0 && (
        <EmptyState
          className="py-20"
          icon={<Plug aria-hidden="true" />}
          title="No integrations available"
          body={isAdmin ? "Install a pack or plugin that provides a connection, then return here to connect your account." : "An admin needs to install a pack before you can connect your account."}
          action={isAdmin ? <ButtonLink href="/admin/settings/packs">Review packs</ButtonLink> : undefined}
        />
      )}

      {query && visibleWorkspace.length === 0 && visibleConnectors.length === 0 && visiblePersonal.length === 0 ? (
        <EmptyState
          className="py-20"
          icon={<Plug aria-hidden="true" />}
          title={variant === "chat" ? "No matching connectors" : "No matching integrations"}
          body="Try another provider, plugin, or scope."
        />
      ) : null}
    </Wrapper>
  );
}
