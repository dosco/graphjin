"use client";

import React, { useMemo, useState } from "react";
import { RotateCcw } from "lucide-react";
import { evaluateExpression, parseExpression, type Expression } from "@neko/llm/work/answer-expression";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import Select from "@/components/Select";
import { Slider } from "@/components/ui/slider";
import { formatUnitValue } from "./answer-format";
import { AskChip } from "./ask";
import { registerComponent, type RenderContext } from "./renderer";
import type { A2UIComponent } from "./types";
import type { CompareProps, WhatIfControl, WhatIfProps } from "./catalog";

const numberOf = (cell: unknown) => {
  const value = Number(String(cell ?? "").replaceAll(",", ""));
  return Number.isFinite(value) ? value : null;
};

function CompareBlock({ props, ctx }: { props: CompareProps & { id: string }; ctx: RenderContext }) {
  const columns = Array.isArray(props.columns) ? props.columns : [];
  const rows = Array.isArray(props.rows) ? props.rows : [];
  const [nameColumn, ...measures] = columns;
  if (!nameColumn || rows.length === 0) return null;
  const peaks = new Map(measures.map((column) => {
    const values = rows.map((row) => numberOf(row[column.key]));
    const numeric = values.every((value) => value !== null);
    return [column.key, numeric ? Math.max(...values.map((value) => Math.abs(value!))) : null];
  }));
  return (
    <section className="answer-compare" aria-label="Comparison" style={{ "--answer-compare-count": rows.length } as React.CSSProperties}>
      {rows.map((row, index) => {
        const name = String(row[nameColumn.key] ?? "");
        return (
          <article className="answer-compare-panel" key={`${name}-${index}`}>
            <header className="answer-compare-head">
              <h3>{name}</h3>
            </header>
            <dl>
              {measures.map((column) => {
                const peak = peaks.get(column.key);
                const value = numberOf(row[column.key]);
                const share = peak && value !== null ? Math.abs(value) / peak : null;
                return (
                  <div className="answer-compare-measure" key={column.key} data-leader={share === 1 && rows.length > 1 ? "" : undefined}>
                    <dt>{column.label}</dt>
                    <dd>{String(row[column.key] ?? "")}</dd>
                    {share !== null ? (
                      <span className="answer-compare-bar" aria-hidden="true">
                        <span style={{ transform: `scaleX(${share})` }} />
                      </span>
                    ) : null}
                  </div>
                );
              })}
            </dl>
            <AskChip ctx={ctx} componentId={props.id} template={props.drill} label={name} className="answer-compare-ask" />
          </article>
        );
      })}
    </section>
  );
}

registerComponent("Compare", (comp: A2UIComponent, ctx: RenderContext) => (
  <CompareBlock key={comp.id} props={comp as unknown as CompareProps & { id: string }} ctx={ctx} />
));

function parseAll(outputs: WhatIfProps["outputs"]): Array<Expression | null> {
  return outputs.map((output) => {
    try {
      return parseExpression(output.expression);
    } catch {
      return null;
    }
  });
}

function ControlRow({ control, value, onChange }: { control: WhatIfControl; value: number; onChange: (value: number) => void }) {
  const id = `whatif-${control.name}`;
  const clamp = (next: number) => Math.min(control.max, Math.max(control.min, next));
  return (
    <div className="answer-whatif-control">
      <div className="answer-whatif-control-head">
        <label htmlFor={id}>{control.label}</label>
        <Input
          id={id}
          type="number"
          inputMode="decimal"
          className="answer-whatif-number"
          value={value}
          min={control.min}
          max={control.max}
          step={control.step}
          onChange={(event) => {
            const next = Number(event.target.value);
            if (Number.isFinite(next)) onChange(clamp(next));
          }}
        />
        {control.unit ? <span className="answer-whatif-unit">{control.unit}</span> : null}
      </div>
      <Slider
        value={[value]}
        min={control.min}
        max={control.max}
        step={control.step}
        thumbLabel={control.label}
        onValueChange={([next]) => onChange(next ?? value)}
      />
      <div className="answer-whatif-range" aria-hidden="true">
        <span>{formatUnitValue(control.min, control.unit, { compact: false })}</span>
        <span>{formatUnitValue(control.max, control.unit, { compact: false })}</span>
      </div>
    </div>
  );
}

function ChoiceRow({ control, picked, onPick }: { control: WhatIfControl; picked: number; onPick: (index: number) => void }) {
  const id = `whatif-${control.name}`;
  return (
    <div className="answer-whatif-control">
      <div className="answer-whatif-control-head">
        <label htmlFor={id}>{control.label}</label>
      </div>
      <Select
        id={id}
        value={String(picked)}
        onChange={(next) => onPick(Number(next))}
        options={control.options!.map((option, index) => ({ value: String(index), label: option.label }))}
      />
    </div>
  );
}

function WhatIfBlock({ props }: { props: WhatIfProps & { id: string } }) {
  const controls = useMemo(() => (Array.isArray(props.controls) ? props.controls : []), [props.controls]);
  const outputs = useMemo(() => (Array.isArray(props.outputs) ? props.outputs : []), [props.outputs]);
  const defaults = useMemo(() => Object.fromEntries(controls.map((control) => [control.name, control.value])), [controls]);
  const [settings, setSettings] = useState<Record<string, number>>(defaults);
  const [picks, setPicks] = useState<Record<string, number>>({});
  const expressions = useMemo(() => parseAll(outputs), [outputs]);
  const fixed = props.values ?? {};
  const results = expressions.map((expression) => expression ? evaluateExpression(expression, { ...fixed, ...settings }) : Number.NaN);
  const baseline = expressions.map((expression) => expression ? evaluateExpression(expression, { ...fixed, ...defaults }) : Number.NaN);
  const changed = controls.some((control) => control.options ? (picks[control.name] ?? 0) !== 0 : settings[control.name] !== control.value);
  if (controls.length === 0 || outputs.length === 0) return null;
  return (
    <section className="answer-whatif" aria-label="What-if">
      <div className="answer-whatif-controls">
        <div className="answer-whatif-eyebrow">
          <span>Adjust</span>
          <Button variant="ghost" size="sm" disabled={!changed} onClick={() => { setSettings(defaults); setPicks({}); }}>
            <RotateCcw aria-hidden="true" strokeWidth={1.9} />
            Reset
          </Button>
        </div>
        {controls.map((control) => control.options?.length ? (
          <ChoiceRow
            key={control.name}
            control={control}
            picked={picks[control.name] ?? 0}
            onPick={(index) => {
              setPicks((current) => ({ ...current, [control.name]: index }));
              setSettings((current) => ({ ...current, [control.name]: control.options![index]!.value }));
            }}
          />
        ) : (
          <ControlRow
            key={control.name}
            control={control}
            value={settings[control.name] ?? control.value}
            onChange={(value) => setSettings((current) => ({ ...current, [control.name]: value }))}
          />
        ))}
      </div>
      <dl className="answer-whatif-results" aria-live="polite">
        {outputs.map((output, index) => {
          const delta = results[index]! - baseline[index]!;
          return (
            <div className="answer-whatif-result" key={`${output.label}-${index}`}>
              <dt>{output.label}</dt>
              <dd>{formatUnitValue(results[index]!, output.unit)}</dd>
              <div className="answer-whatif-delta" data-direction={!changed || delta === 0 ? "flat" : delta > 0 ? "up" : "down"}>
                {!changed || !Number.isFinite(delta) || delta === 0
                  ? "Starting point"
                  : `${delta > 0 ? "+" : "−"}${formatUnitValue(Math.abs(delta), output.unit === "%" ? "pts" : output.unit)} from the starting point`}
              </div>
            </div>
          );
        })}
      </dl>
    </section>
  );
}

registerComponent("WhatIf", (comp: A2UIComponent) => (
  <WhatIfBlock key={comp.id} props={comp as unknown as WhatIfProps & { id: string }} />
));
