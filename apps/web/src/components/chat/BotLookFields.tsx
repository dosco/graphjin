"use client";

import { nekoCoats, nekoPatterns, type NekoPattern } from "@neko/avatars/react";
import { FieldInput } from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { BotAvatar } from "./BotAvatar";

const PATTERN_LABELS: Record<NekoPattern, string> = {
  solid: "Solid",
  patch: "Patch",
  tabby: "Tabby",
};

export type BotLook = { name: string; coat: string; pattern: NekoPattern };

export function BotLookFields({
  look,
  onChange,
  error,
  autoFocus = false,
}: {
  look: BotLook;
  onChange: (look: BotLook) => void;
  error?: string | null;
  autoFocus?: boolean;
}) {
  return (
    <>
      <FieldInput
        label="Name"
        placeholder="Name your bot"
        value={look.name}
        maxLength={40}
        autoFocus={autoFocus}
        onChange={(event) => onChange({ ...look, name: event.target.value })}
        error={error ?? undefined}
      />

      <div className="chat-create-group">
        <span className="chat-create-label" id="coat-label">Coat</span>
        <ToggleGroup
          type="single"
          value={look.coat}
          onValueChange={(value) => value && onChange({ ...look, coat: value })}
          aria-labelledby="coat-label"
          className="chat-create-swatches"
        >
          {nekoCoats.map((c) => (
            <ToggleGroupItem key={c.name} value={c.name} aria-label={c.name} className="chat-create-swatch">
              <BotAvatar coat={c.name} pattern={look.pattern} size={40} animated={false} aria-hidden />
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <div className="chat-create-group">
        <span className="chat-create-label" id="pattern-label">Pattern</span>
        <ToggleGroup
          type="single"
          value={look.pattern}
          onValueChange={(value) => value && onChange({ ...look, pattern: value as NekoPattern })}
          aria-labelledby="pattern-label"
          className="chat-create-swatches"
        >
          {nekoPatterns.map((p) => (
            <ToggleGroupItem key={p} value={p} className="chat-create-swatch is-labelled">
              <BotAvatar coat={look.coat} pattern={p} size={40} animated={false} aria-hidden />
              <span>{PATTERN_LABELS[p]}</span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
    </>
  );
}
