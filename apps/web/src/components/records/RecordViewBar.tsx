"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { BookmarkPlus, ListFilter, X } from "lucide-react";
import type {
  RecordListFilter,
  RecordSavedView,
  RecordSavedViewDefinition,
} from "@neko/records";
import { RecordFilterPopover } from "./RecordFilterBuilder";
import { Button, IconButton } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Segment, SegmentedControl } from "@/components/ui/tabs";
import {
  buildFilter,
  describeLeaf,
  filterClauses,
  hrefWithQuery,
  PAGING_RESET,
  type FilterMatch,
  type RecordFilterField,
} from "@/lib/records-filter";

export function RecordViewBar({
  base,
  objectLabel,
  query,
  ownerScoped,
  appId,
  objectApiName,
  views,
  selectedView,
  definition,
  fields,
  canShare,
}: {
  base: string;
  objectLabel: string;
  query: Record<string, string | undefined>;
  ownerScoped: boolean;
  appId: string;
  objectApiName: string;
  views: RecordSavedView[];
  selectedView: RecordSavedView | null;
  definition: RecordSavedViewDefinition;
  fields: RecordFilterField[];
  canShare: boolean;
}) {
  const router = useRouter();
  const [saveOpen, setSaveOpen] = useState(false);
  const mine = query.mine === "true";
  const endpoint = `/api/a/${encodeURIComponent(appId)}/${encodeURIComponent(objectApiName)}/views`;
  const { match, clauses } = filterClauses(definition.filter);
  const byName = new Map(fields.map((field) => [field.apiName, field]));

  function applyFilter(nextMatch: FilterMatch, next: RecordListFilter[]) {
    const filter = buildFilter(nextMatch, next);
    router.push(
      hrefWithQuery(base, query, {
        ...PAGING_RESET,
        filter: filter ? JSON.stringify(filter) : "null",
      }),
    );
  }

  return (
    <div className="records-viewbar">
      <NativeSelect
        aria-label="Saved view"
        className="records-view-select"
        value={selectedView?.id ?? ""}
        onChange={(event) =>
          router.push(event.target.value ? `${base}?view=${encodeURIComponent(event.target.value)}` : base)
        }
      >
        <option value="">All {objectLabel.toLowerCase()}</option>
        {views.map((view) => (
          <option value={view.id} key={view.id}>
            {view.label}
            {view.shared ? " · shared" : ""}
          </option>
        ))}
      </NativeSelect>
      {clauses.length > 1 && (
        <SegmentedControl aria-label="How filters combine">
          <Segment selected={match === "and"} onClick={() => match !== "and" && applyFilter("and", clauses)}>
            Match all
          </Segment>
          <Segment selected={match === "or"} onClick={() => match !== "or" && applyFilter("or", clauses)}>
            Match any
          </Segment>
        </SegmentedControl>
      )}
      {clauses.map((clause, index) => {
        const field = byName.get(clause.field);
        const fieldLabel = field?.label ?? clause.field.replaceAll("_", " ");
        const summary = describeLeaf(clause, field);
        return (
          <span className="records-filter-chip" key={`${clause.field}-${index}-${summary}`}>
            <RecordFilterPopover
              appId={appId}
              fields={fields}
              initial={clause}
              onApply={(leaf) =>
                applyFilter(match, clauses.map((current, position) => (position === index ? leaf : current)))
              }
              trigger={
                <Button variant="ghost" size="sm" className="records-filter-chip-edit" aria-label={`Edit filter: ${fieldLabel} ${summary}`}>
                  {fieldLabel} <b>{summary}</b>
                </Button>
              }
            />
            <IconButton
              size="icon-sm"
              variant="ghost"
              label={`Remove filter: ${fieldLabel} ${summary}`}
              onClick={() => applyFilter(match, clauses.filter((_, position) => position !== index))}
            >
              <X aria-hidden="true" />
            </IconButton>
          </span>
        );
      })}
      <RecordFilterPopover
        appId={appId}
        fields={fields}
        onApply={(leaf) => applyFilter(match, [...clauses, leaf])}
        trigger={
          <Button size="sm" variant="secondary">
            <ListFilter aria-hidden="true" /> {clauses.length > 0 ? "Add filter" : "Filter"}
          </Button>
        }
      />
      {clauses.length > 0 && (
        <Button size="sm" variant="ghost" onClick={() => applyFilter(match, [])}>
          Clear filters
        </Button>
      )}
      <Popover open={saveOpen} onOpenChange={setSaveOpen}>
        <PopoverTrigger asChild>
          <Button size="sm" variant="ghost">
            <BookmarkPlus aria-hidden="true" /> Save view
          </Button>
        </PopoverTrigger>
        <PopoverContent aria-label="Save view">
          <form action={endpoint} method="post" className="grid gap-3">
            <Field label="Name" htmlFor="records-save-view-name">
              <Input id="records-save-view-name" name="label" required maxLength={80} defaultValue={selectedView?.label} />
            </Field>
            <input
              type="hidden"
              name="definition"
              value={JSON.stringify(definition)}
              data-ui-bespoke-reason="hidden view definition payload"
            />
            {canShare && (
              <Checkbox
                name="shared"
                value="true"
                defaultChecked={selectedView?.shared}
                label="Share with this organization"
              />
            )}
            <Button variant="primary" type="submit">
              Save view
            </Button>
          </form>
        </PopoverContent>
      </Popover>
      {selectedView && (!selectedView.shared || canShare) && (
        <form action={`${endpoint}/${selectedView.id}`} method="post">
          <Button variant="danger" size="sm" type="submit" aria-label={`Delete ${selectedView.label}`}>
            Delete view
          </Button>
        </form>
      )}
      {ownerScoped && (
        <Link
          href={hrefWithQuery(base, query, { ...PAGING_RESET, mine: mine ? "false" : "true" })}
          className="records-scope-toggle"
          role="switch"
          aria-checked={mine}
        >
          My records
          <span className={`records-switch${mine ? " is-on" : ""}`} aria-hidden="true">
            <span />
          </span>
        </Link>
      )}
    </div>
  );
}
