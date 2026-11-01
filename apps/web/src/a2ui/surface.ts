/** Immutable A2UI v0.9/v1.0 surface reducer and data-binding helpers. */

import type {
  A2UIComponent,
  A2UIMessage,
  A2UIVersion,
  DynamicValue,
  SurfaceState,
} from "./types";

function pointerParts(pointer: string): string[] {
  return pointer
    .replace(/^\//, "")
    .split("/")
    .filter(Boolean)
    .map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
}

/** Resolve a JSON Pointer path against an object. */
export function resolvePointer(obj: Record<string, unknown>, pointer: string): unknown {
  if (!pointer || pointer === "/") return obj;
  let current: unknown = obj;
  for (const part of pointerParts(pointer)) {
    if (current == null || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

/** Set or delete a JSON Pointer path, returning a cloned model. */
export function setDataModelValue(
  obj: Record<string, unknown>,
  pointer: string,
  value: unknown,
  deleteValue: unknown = Symbol("never-delete"),
): Record<string, unknown> {
  if (!pointer || pointer === "/") {
    return value && typeof value === "object" && !Array.isArray(value)
      ? structuredClone(value as Record<string, unknown>)
      : {};
  }
  const result = structuredClone(obj);
  const parts = pointerParts(pointer);
  if (parts.length === 0) return result;
  let current: Record<string, unknown> = result;
  for (let i = 0; i < parts.length - 1; i++) {
    if (current[parts[i]] == null || typeof current[parts[i]] !== "object") {
      current[parts[i]] = {};
    }
    current = current[parts[i]] as Record<string, unknown>;
  }
  if (Object.is(value, deleteValue)) delete current[parts.at(-1)!];
  else current[parts.at(-1)!] = value;
  return result;
}

/** Resolve a DynamicValue. */
export function resolveDynamic<T>(val: DynamicValue<T>, dataModel: Record<string, unknown>): T {
  if (val != null && typeof val === "object" && !Array.isArray(val) && "path" in val) {
    return resolvePointer(dataModel, (val as { path: string }).path) as T;
  }
  return val as T;
}

function resolveValue(value: unknown, dataModel: Record<string, unknown>): unknown {
  if (value == null || typeof value !== "object") return value;
  if (!Array.isArray(value) && "path" in value && typeof value.path === "string") {
    return resolvePointer(dataModel, value.path);
  }
  if (Array.isArray(value)) return value.map((item) => resolveValue(item, dataModel));
  return Object.fromEntries(
    Object.entries(value).map(([key, item]) => [key, resolveValue(item, dataModel)]),
  );
}

/** Resolve bindings recursively, including action event context values. */
export function resolveComponent(
  component: A2UIComponent,
  dataModel: Record<string, unknown>,
): A2UIComponent {
  const resolved: A2UIComponent = { id: component.id, component: component.component };
  for (const [key, value] of Object.entries(component)) {
    if (key === "id" || key === "component") continue;
    resolved[key] = resolveValue(value, dataModel);
  }
  return resolved;
}

function flattenComponent(comp: A2UIComponent): A2UIComponent[] {
  const out: A2UIComponent[] = [];
  const normalized: A2UIComponent = { ...comp };
  if (Array.isArray(comp.children)) {
    const children: string[] = [];
    for (const child of comp.children) {
      if (typeof child === "string") children.push(child);
      else if (child && typeof child === "object" && "id" in child) {
        const nested = child as A2UIComponent;
        children.push(nested.id);
        out.push(...flattenComponent(nested));
      }
    }
    normalized.children = children;
  }
  out.push(normalized);
  return out;
}

function addComponents(surface: SurfaceState, components: A2UIComponent[]): SurfaceState {
  const updated = { ...surface, components: new Map(surface.components) };
  for (const comp of components) {
    if (!comp || typeof comp !== "object" || typeof comp.id !== "string" || typeof comp.component !== "string") continue;
    for (const flat of flattenComponent(comp)) updated.components.set(flat.id, flat);
  }
  return updated;
}

export function createSurface(
  surfaceId: string,
  catalogId: string,
  theme?: Record<string, unknown>,
  version: A2UIVersion = "v0.9",
): SurfaceState {
  return { version, surfaceId, catalogId, components: new Map(), dataModel: {}, theme };
}

export function applyMessage(
  surfaces: Map<string, SurfaceState>,
  message: A2UIMessage,
): Map<string, SurfaceState> {
  const next = new Map(surfaces);

  if ("createSurface" in message) {
    const params = message.createSurface;
    let surface: SurfaceState = {
      ...createSurface(params.surfaceId, params.catalogId, params.theme, message.version),
      surfaceProperties: params.surfaceProperties,
      sendDataModel: params.sendDataModel,
      dataModel: structuredClone(params.dataModel ?? {}),
    };
    if (params.components) surface = addComponents(surface, params.components);
    next.set(params.surfaceId, surface);
  }

  if ("updateComponents" in message) {
    const { surfaceId, components } = message.updateComponents;
    const surface = next.get(surfaceId);
    if (!surface) return next;
    next.set(surfaceId, addComponents(surface, components));
  }

  if ("updateDataModel" in message) {
    const { surfaceId, path, value } = message.updateDataModel;
    const surface = next.get(surfaceId);
    if (!surface) return next;
    const deleteSentinel = message.version === "v1.0" ? null : undefined;
    next.set(surfaceId, {
      ...surface,
      dataModel: setDataModelValue(surface.dataModel, path ?? "/", value, deleteSentinel),
    });
  }

  if ("deleteSurface" in message) next.delete(message.deleteSurface.surfaceId);
  return next;
}

export function getResolvedComponents(surface: SurfaceState): A2UIComponent[] {
  return Array.from(surface.components.values()).map((component) =>
    resolveComponent(component, surface.dataModel),
  );
}

export function getRootComponent(surface: SurfaceState): A2UIComponent | undefined {
  return surface.components.get("root");
}

export function bodyChildIds(surface: SurfaceState, root: A2UIComponent): string[] {
  const declared = Array.isArray(root.children) ? (root.children as string[]) : [];
  if (declared.length > 0 || root.id !== "root") return declared;
  const referenced = new Set<string>();
  for (const component of surface.components.values()) {
    if (Array.isArray(component.children)) {
      for (const child of component.children) {
        if (typeof child === "string") referenced.add(child);
      }
    }
    if (typeof component.child === "string") referenced.add(component.child);
    if (Array.isArray(component.tabs)) {
      for (const tab of component.tabs) {
        if (tab && typeof tab === "object" && typeof tab.child === "string") {
          referenced.add(tab.child);
        }
      }
    }
  }
  return Array.from(surface.components.keys()).filter(
    (id) => id !== root.id && !referenced.has(id),
  );
}
