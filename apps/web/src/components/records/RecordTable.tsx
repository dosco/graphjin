import Link from "next/link";
import type { RecordObjectView } from "@neko/records";
import type { RecordOwnerIdentity } from "@/lib/records";
import type { PendingRecordAction } from "@/lib/records-pending";
import {
  recordReferenceIdentityKey,
  type RecordReferenceIdentity,
} from "@/lib/records-reference";
import { RecordCell } from "./RecordCell";
import { PendingChangeMarker } from "./PendingChangeMarker";
import { buttonClassName } from "@/components/ui/button";
import { hrefWithQuery as hrefWith, PAGING_RESET } from "@/lib/records-filter";
import { RecordPageSize } from "./RecordPageSize";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function RecordTable({
  appId,
  view,
  rows,
  owners,
  pendingActions = {},
  references = {},
  total,
  cursor,
  page,
  pageSize = 50,
  query,
}: {
  appId: string;
  view: RecordObjectView;
  rows: Array<Record<string, unknown>>;
  owners: Record<string, RecordOwnerIdentity>;
  pendingActions?: Record<string, PendingRecordAction[]>;
  references?: Record<string, RecordReferenceIdentity>;
  total: number;
  cursor: string | null;
  page: number;
  pageSize?: number;
  query: Record<string, string | undefined>;
}) {
  const base = `/a/${appId}/${view.object.apiName}`;
  const shownFrom = (page - 1) * pageSize + 1;
  const shownThrough = (page - 1) * pageSize + rows.length;
  const hasMore = cursor !== null && shownThrough < total;
  const trail = query.back ? query.back.split(",") : [];
  const previous =
    page <= 1
      ? null
      : page === 2
        ? hrefWith(base, query, PAGING_RESET)
        : hrefWith(base, query, {
            after: trail.at(-1) ?? null,
            page: String(page - 1),
            back: trail.length > 1 ? trail.slice(0, -1).join(",") : null,
          });
  const next = hasMore
    ? hrefWith(base, query, {
        after: cursor,
        page: String(page + 1),
        back: page >= 2 && query.after ? [...trail, query.after].join(",") : null,
      })
    : null;
  return (
    <section
      className="records-list-card"
      aria-label={`${view.object.pluralLabel} records`}
    >
      <div className="records-table-scroll">
        <Table className="records-table">
          <TableHeader>
            <TableRow>
              {view.columns.map((column) => {
                const active = query.sort === column.apiName;
                const direction =
                  active && query.direction === "desc" ? "asc" : "desc";
                const sortable =
                  column.kind !== "owner" && column.kind !== "system_datetime";
                return (
                  <TableHead
                    key={column.apiName}
                    className={
                      ["integer", "decimal", "currency", "percent"].includes(
                        column.kind,
                      )
                        ? "is-number"
                        : undefined
                    }
                  >
                    {sortable ? (
                      <Link
                        href={hrefWith(base, query, {
                          ...PAGING_RESET,
                          sort: column.apiName,
                          direction,
                        })}
                      >
                        {column.label}
                        {active && (
                          <span aria-hidden="true">
                            {" "}
                            {query.direction === "desc" ? "↓" : "↑"}
                          </span>
                        )}
                      </Link>
                    ) : (
                      column.label
                    )}
                  </TableHead>
                );
              })}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, rowIndex) => {
              const recordId = String(row.id ?? "");
              const recordPendingActions = pendingActions[recordId] ?? [];
              return (
                <TableRow
                  key={recordId || `row-${rowIndex}`}
                  className={
                    recordPendingActions.length > 0 ? "is-pending" : undefined
                  }
                >
                  {view.columns.map((column) => {
                    const value = row[column.columnName];
                    const ownerId =
                      column.kind === "owner" && typeof value === "string"
                        ? value
                        : null;
                    const nameColumn =
                      column.columnName === view.object.nameField;
                    const content = (
                      <RecordCell
                        appId={appId}
                        column={column}
                        value={value}
                        owner={ownerId ? owners[ownerId] : undefined}
                        reference={
                          references[
                            recordReferenceIdentityKey(
                              column.apiName,
                              String(value ?? ""),
                            )
                          ]
                        }
                        linkify={!nameColumn}
                      />
                    );
                    return (
                      <TableCell
                        key={column.apiName}
                        className={
                          [
                            "integer",
                            "decimal",
                            "currency",
                            "percent",
                          ].includes(column.kind)
                            ? "is-number"
                            : undefined
                        }
                      >
                        {nameColumn && recordId ? (
                          <span className="records-name-cell">
                            <Link
                              className="records-name-link"
                              href={`${base}/${encodeURIComponent(recordId)}`}
                            >
                              {content}
                            </Link>
                            <PendingChangeMarker
                              actions={recordPendingActions}
                            />
                          </span>
                        ) : (
                          content
                        )}
                      </TableCell>
                    );
                  })}
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      {rows.length === 0 && (
        <div className="records-empty">
          <strong>No matching {view.object.pluralLabel.toLowerCase()}</strong>
          <span>Change the current search or create the first record.</span>
        </div>
      )}
      <footer className="records-list-foot">
        <span className="records-list-range">
          {total === 0
            ? "No records"
            : `Showing ${shownFrom.toLocaleString("en")}–${shownThrough.toLocaleString("en")} of ${total.toLocaleString("en")}`}
        </span>
        <RecordPageSize base={base} query={query} size={pageSize} />
        <nav className="records-pager" aria-label="Pages">
          {previous ? (
            <Link className={buttonClassName({ size: "sm" })} href={previous} rel="prev">
              <span aria-hidden="true">←</span> Previous
            </Link>
          ) : (
            <span className={buttonClassName({ size: "sm" })} aria-disabled="true">
              <span aria-hidden="true">←</span> Previous
            </span>
          )}
          <span className="records-page-number">Page {page}</span>
          {next ? (
            <Link className={buttonClassName({ size: "sm" })} href={next} rel="next">
              Next <span aria-hidden="true">→</span>
            </Link>
          ) : (
            <span className={buttonClassName({ size: "sm" })} aria-disabled="true">
              Next <span aria-hidden="true">→</span>
            </span>
          )}
        </nav>
      </footer>
    </section>
  );
}
