"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import type { UserGroupRow } from "@neko/db";
import { adminApi } from "@/components/admin/admin-api";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Segment, SegmentedControl } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export type DataAccessRuleView = {
  id: string;
  source: string;
  tableSchema: string;
  tableName: string;
  columns: string[];
  rowFilter: unknown;
  updatedAt: string;
};

type SourceTable = { schema: string; table: string; columns: string[] };
type Condition = { column: string; op: string; value: string; variable: string };

const OPS = [
  { op: "eq", label: "equals" },
  { op: "neq", label: "does not equal" },
  { op: "gt", label: "greater than" },
  { op: "gte", label: "at least" },
  { op: "lt", label: "less than" },
  { op: "lte", label: "at most" },
  { op: "in", label: "is one of" },
  { op: "nin", label: "is not one of" },
  { op: "is_null", label: "is empty" },
];

function literal(value: string): string | number | boolean {
  const trimmed = value.trim();
  if (trimmed === "true" || trimmed === "false") return trimmed === "true";
  if (trimmed !== "" && !Number.isNaN(Number(trimmed))) return Number(trimmed);
  return trimmed;
}

function conditionFilter(c: Condition) {
  if (c.op === "is_null") return { column: c.column, op: c.op, value: c.value !== "false" };
  if (c.variable) return { column: c.column, op: c.op, value: { var: c.variable } };
  if (c.op === "in" || c.op === "nin") return { column: c.column, op: c.op, value: c.value.split(",").map(literal) };
  return { column: c.column, op: c.op, value: literal(c.value) };
}

function describeFilter(filter: unknown): string {
  if (!filter || typeof filter !== "object") return "All rows";
  const node = filter as Record<string, unknown>;
  if (Array.isArray(node.and)) return node.and.map(describeFilter).join(" and ");
  if (Array.isArray(node.or)) return `(${node.or.map(describeFilter).join(" or ")})`;
  const value = node.value as unknown;
  const shown =
    value && typeof value === "object" && !Array.isArray(value)
      ? `$${(value as { var: string }).var}`
      : Array.isArray(value)
        ? value.join(", ")
        : String(value);
  return `${String(node.column)} ${OPS.find((o) => o.op === node.op)?.label ?? String(node.op)} ${shown}`;
}

export function DataAccessSection({
  group,
  rules,
  enabled,
  onError,
}: {
  group: UserGroupRow;
  rules: DataAccessRuleView[];
  enabled: boolean;
  onError: (message: string | null) => void;
}) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [sources, setSources] = useState<Array<{ id: string; label: string }> | null>(null);
  const [source, setSource] = useState("");
  const [tables, setTables] = useState<SourceTable[]>([]);
  const [tableKey, setTableKey] = useState("");
  const [columns, setColumns] = useState<string[]>([]);
  const [match, setMatch] = useState<"and" | "or">("and");
  const [conditions, setConditions] = useState<Condition[]>([]);
  const table = tables.find((t) => `${t.schema}.${t.table}` === tableKey);

  async function run(key: string, call: () => Promise<{ ok: boolean; error?: string }>) {
    setBusy(key);
    onError(null);
    const result = await call();
    setBusy(null);
    if (!result.ok) return onError(result.error ?? "Request failed");
    router.refresh();
  }

  async function openEditor() {
    const result = await adminApi<{ items: Array<{ id: string; label: string }> }>("/api/admin/items?type=data_source");
    const items = result.ok ? result.body.items : [];
    setSources(items);
    if (items[0]) void chooseSource(items[0].id);
  }

  async function chooseSource(next: string) {
    setSource(next);
    setTableKey("");
    setColumns([]);
    setConditions([]);
    const result = await adminApi<{ tables: SourceTable[] }>(`/api/admin/data-access/tables?source=${encodeURIComponent(next)}`);
    setTables(result.ok ? result.body.tables : []);
  }

  function chooseTable(key: string) {
    setTableKey(key);
    const next = tables.find((t) => `${t.schema}.${t.table}` === key);
    setColumns(next ? [...next.columns] : []);
    setConditions([]);
  }

  async function save() {
    if (!table) return;
    const filters = conditions.filter((c) => c.column).map(conditionFilter);
    const rowFilter = filters.length === 0 ? null : filters.length === 1 ? filters[0] : { [match]: filters };
    await run("save", () =>
      adminApi("/api/admin/data-access", "POST", {
        groupId: group.id,
        source,
        tableSchema: table.schema,
        tableName: table.table,
        columns,
        rowFilter,
      }),
    );
  }

  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Data access</h2>
          <p className="settings-card-copy">
            Choose the tables, columns and rows this group reads through GraphJin. A rule applies only when the group holds the data source. A user in several groups reads what any of their groups allows, never more than one group allows on a row.
          </p>
        </div>
        <div className="settings-source">
          <strong className={enabled ? "is-ok" : ""}>{enabled ? "On" : "Off"}</strong>
        </div>
      </div>

      {!enabled && (
        <div className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-inner border border-border px-4 py-3 text-sm text-text2">
          <span>
            Group data access is off, so every member keeps today&apos;s access. Turning it on restarts GraphJin once. Everyone first gets read access to every existing table and column.
          </span>
          <Button
            size="sm"
            variant="primary"
            disabled={busy === "enable"}
            onClick={() => run("enable", () => adminApi("/api/admin/data-access/settings", "PATCH", { enabled: true }))}
          >
            {busy === "enable" ? "Turning on…" : "Turn on"}
          </Button>
        </div>
      )}

      {rules.length > 0 && (
        <div className="mb-5 overflow-x-auto">
          <Table className="w-full border-collapse text-left text-sm">
            <TableHeader>
              <TableRow>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Table</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Columns</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Rows</TableHead>
                <TableHead className="border-b border-border px-3 py-2 font-bold">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.map((rule) => (
                <TableRow key={rule.id} className="border-b border-border last:border-0">
                  <TableCell className="px-3 py-3">
                    <div className="font-semibold text-text">{rule.tableSchema ? `${rule.tableSchema}.${rule.tableName}` : rule.tableName}</div>
                    <div className="text-xs text-text3">{rule.source}</div>
                  </TableCell>
                  <TableCell className="px-3 py-3 text-text2">{rule.columns.join(", ")}</TableCell>
                  <TableCell className="px-3 py-3 text-text2">{describeFilter(rule.rowFilter)}</TableCell>
                  <TableCell className="px-3 py-3">
                    <Button size="sm" variant="danger" disabled={busy === rule.id} onClick={() => run(rule.id, () => adminApi(`/api/admin/data-access/${rule.id}`, "DELETE"))}>
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {sources === null ? (
        <Button onClick={() => void openEditor()}>Add table access</Button>
      ) : sources.length === 0 ? (
        <p className="text-sm text-text3">GraphJin reports no data sources.</p>
      ) : (
        <div className="flex flex-col gap-4 rounded-inner border border-border px-4 py-4">
          <div className="grid grid-cols-2 gap-3 max-[640px]:grid-cols-1">
            <Field label="Data source" htmlFor="rule-source">
              <NativeSelect id="rule-source" value={source} onChange={(e) => void chooseSource(e.target.value)}>
                {sources.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
              </NativeSelect>
            </Field>
            <Field label="Table" htmlFor="rule-table">
              <NativeSelect id="rule-table" value={tableKey} onChange={(e) => chooseTable(e.target.value)}>
                <option value="">Choose a table</option>
                {tables.map((t) => (
                  <option key={`${t.schema}.${t.table}`} value={`${t.schema}.${t.table}`}>{t.schema ? `${t.schema}.${t.table}` : t.table}</option>
                ))}
              </NativeSelect>
            </Field>
          </div>

          {table && (
            <>
              <fieldset>
                <legend className="mb-2 text-sm font-semibold text-text">Columns</legend>
                <div className="grid grid-cols-3 gap-2 max-[640px]:grid-cols-2">
                  {table.columns.map((column) => (
                    <Checkbox
                      key={column}
                      label={column}
                      checked={columns.includes(column)}
                      onCheckedChange={(checked) =>
                        setColumns((current) => (checked === true ? [...current, column] : current.filter((c) => c !== column)))
                      }
                    />
                  ))}
                </div>
              </fieldset>

              <fieldset className="flex flex-col gap-3">
                <legend className="mb-2 text-sm font-semibold text-text">Rows</legend>
                {conditions.length === 0 ? (
                  <p className="text-sm text-text3">All rows. Add a condition to limit rows.</p>
                ) : (
                  <SegmentedControl aria-label="Match conditions">
                    <Segment selected={match === "and"} onClick={() => setMatch("and")}>Match all</Segment>
                    <Segment selected={match === "or"} onClick={() => setMatch("or")}>Match any</Segment>
                  </SegmentedControl>
                )}
                {conditions.map((condition, index) => {
                  const update = (patch: Partial<Condition>) =>
                    setConditions((current) => current.map((c, i) => (i === index ? { ...c, ...patch } : c)));
                  const listOp = condition.op === "in" || condition.op === "nin";
                  return (
                    <div key={index} className="grid grid-cols-[1fr_1fr_1fr_auto] items-end gap-2 max-[720px]:grid-cols-1">
                      <NativeSelect aria-label="Column" value={condition.column} onChange={(e) => update({ column: e.target.value })}>
                        {table.columns.map((c) => <option key={c} value={c}>{c}</option>)}
                      </NativeSelect>
                      <NativeSelect aria-label="Operator" value={condition.op} onChange={(e) => update({ op: e.target.value, variable: "", value: "" })}>
                        {OPS.map((o) => <option key={o.op} value={o.op}>{o.label}</option>)}
                      </NativeSelect>
                      {condition.op === "is_null" ? (
                        <NativeSelect aria-label="Value" value={condition.value || "true"} onChange={(e) => update({ value: e.target.value })}>
                          <option value="true">yes</option>
                          <option value="false">no</option>
                        </NativeSelect>
                      ) : (
                        <div className="flex gap-2">
                          <NativeSelect aria-label="Value kind" value={condition.variable} onChange={(e) => update({ variable: e.target.value })}>
                            <option value="">Value</option>
                            {listOp ? <option value="user_groups">The user&apos;s groups</option> : <option value="user_id">The user&apos;s id</option>}
                          </NativeSelect>
                          {!condition.variable && (
                            <Input
                              aria-label="Value"
                              value={condition.value}
                              placeholder={listOp ? "emea, apac" : "emea"}
                              onChange={(e) => update({ value: e.target.value })}
                            />
                          )}
                        </div>
                      )}
                      <Button size="sm" variant="ghost" onClick={() => setConditions((current) => current.filter((_, i) => i !== index))}>Remove</Button>
                    </div>
                  );
                })}
                <div>
                  <Button size="sm" onClick={() => setConditions((current) => [...current, { column: table.columns[0] ?? "", op: "eq", value: "", variable: "" }])}>
                    Add condition
                  </Button>
                </div>
              </fieldset>

              <ActionGroup align="start">
                <Button variant="primary" disabled={busy === "save" || columns.length === 0} onClick={() => void save()}>
                  {busy === "save" ? "Saving…" : "Save table access"}
                </Button>
                <Button variant="ghost" onClick={() => setSources(null)}>Cancel</Button>
              </ActionGroup>
            </>
          )}
        </div>
      )}
    </section>
  );
}
