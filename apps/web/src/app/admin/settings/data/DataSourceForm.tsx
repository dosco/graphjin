"use client";

import { useState } from "react";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";

type DataSourcePayload = {
  source: "org" | "unset";
  kind: string;
  graphqlUrl: string;
  mcpUrl: string;
  label: string;
};

const GRAPHQL_SUFFIX = "/api/v1/graphql";
const MCP_SUFFIX = "/api/v1/mcp";

function deriveEndpoints(rootUrl: string): { graphqlUrl: string; mcpUrl: string } {
  const root = deriveRoot(rootUrl);
  return { graphqlUrl: `${root}${GRAPHQL_SUFFIX}`, mcpUrl: `${root}${MCP_SUFFIX}` };
}

// Accept whatever the user pastes — bare root, trailing slash, or a full
// GraphJin endpoint URL — and reduce it to a clean root so deriveEndpoints
// can append the canonical suffixes without doubling them.
function deriveRoot(input: string): string {
  let s = input.trim().replace(/\/+$/, "");
  const lower = s.toLowerCase();
  if (lower.endsWith(GRAPHQL_SUFFIX)) s = s.slice(0, -GRAPHQL_SUFFIX.length);
  else if (lower.endsWith(MCP_SUFFIX)) s = s.slice(0, -MCP_SUFFIX.length);
  return s.replace(/\/+$/, "");
}

export default function DataSourceForm({ initial }: { initial: DataSourcePayload }) {
  const [data, setData] = useState({
    rootUrl: deriveRoot(initial.graphqlUrl),
    label: initial.label || "primary",
  });
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  async function test() {
    setTesting(true);
    try {
      const { graphqlUrl, mcpUrl } = deriveEndpoints(data.rootUrl);
      const res = await fetch("/api/settings/data-source/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ graphqlUrl, mcpUrl }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Test failed");
      toast.success(
        body.mcpOk === false ? "GraphQL reachable. MCP unreachable." : "Connection looks good.",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setTesting(false);
    }
  }

  async function save() {
    setSaving(true);
    try {
      const { graphqlUrl, mcpUrl } = deriveEndpoints(data.rootUrl);
      const res = await fetch("/api/settings/data-source", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ graphqlUrl, mcpUrl, label: data.label }),
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? "Save failed");
      toast.success("Data source saved.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="root is-narrow">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }} />
      <PageHeading
        title="Data source"
        description="Set the GraphJin endpoint OpenNeko uses for business queries and agent tools."
      />

      <section className="settings-card">
        <div className="grid gap-4">
          <Field
            label="GraphJin URL *"
            htmlFor="graphjin-url"
            hint="Just the base URL. OpenNeko handles the GraphQL and MCP endpoints automatically."
          >
            <Input
              id="graphjin-url"
              value={data.rootUrl}
              placeholder="http://localhost:8080"
              onChange={(e) => setData((p) => ({ ...p, rootUrl: e.target.value }))}
            />
          </Field>
          <Field label="Label" htmlFor="data-source-label">
            <Input
              id="data-source-label"
              value={data.label}
              placeholder="primary"
              onChange={(e) => setData((p) => ({ ...p, label: e.target.value }))}
            />
          </Field>
        </div>
        <div className="flex justify-end gap-2.5 mt-5 max-[720px]:flex-col max-[720px]:items-stretch [&>button]:max-[720px]:w-full">
          <Button onClick={test} disabled={testing || !data.rootUrl.trim()}>
            {testing ? "Testing…" : "Test connection"}
          </Button>
          <Button variant="primary" onClick={save} disabled={saving || !data.rootUrl.trim()}>
            {saving ? "Saving…" : "Save settings"}
          </Button>
        </div>
      </section>
    </div>
  );
}
