"use client";

import { Fragment } from "react";
import type { A2UIComponent, SurfaceState } from "./types";
import type { BriefingCardProps, BriefingProps } from "./catalog";
import { resolveComponent } from "./surface";

/**
 * Component Registry
 *
 * Maps A2UI component type names to React render functions.
 * Each render function receives the resolved props (all dynamic values
 * already replaced with actual data) and any callbacks needed.
 */

export type ComponentRenderer = (
  component: A2UIComponent,
  context: RenderContext
) => React.ReactNode;

export interface RenderContext {
  surface: SurfaceState;
  onAction?: (componentId: string, eventName: string, context?: Record<string, unknown>) => void;
  onDataChange?: (path: string, value: unknown) => void;
  // Component-specific callbacks passed through from the page
  extras?: Record<string, unknown>;
}

// Registry of component type → renderer
const registry = new Map<string, ComponentRenderer>();

/** Register a React renderer for an A2UI component type */
export function registerComponent(type: string, renderer: ComponentRenderer) {
  registry.set(type, renderer);
}

/** Render a single A2UI component by resolving its data bindings and looking up its renderer */
export function renderComponent(
  component: A2UIComponent,
  context: RenderContext
): React.ReactNode {
  const resolved = resolveComponent(component, context.surface.dataModel);
  const renderer = registry.get(resolved.component);
  if (!renderer) {
    console.warn(`[A2UI] No renderer for component type: ${resolved.component}`);
    return null;
  }
  return renderer(resolved, context);
}

/** Render all components of a given type from a surface */
export function renderComponentsByType(
  surface: SurfaceState,
  type: string,
  context: RenderContext
): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  let i = 0;
  for (const [id, comp] of surface.components) {
    if (comp.component === type) {
      const node = renderComponent(comp, context);
      if (node != null) nodes.push(<Fragment key={id || `c${i}`}>{node}</Fragment>);
    }
    i += 1;
  }
  return nodes;
}

/** Render child components referenced by a parent's children array */
export function renderChildren(
  childIds: string[],
  context: RenderContext
): React.ReactNode[] {
  return childIds
    .map((id, i) => {
      const comp = context.surface.components.get(id);
      if (!comp) return null;
      const node = renderComponent(comp, context);
      // Components can arrive without an id; fall back to position so the list
      // key is always present and unique.
      return node == null ? null : <Fragment key={id || `c${i}`}>{node}</Fragment>;
    })
    .filter(Boolean);
}

// Re-export types for convenience
export type { BriefingCardProps, BriefingProps };
