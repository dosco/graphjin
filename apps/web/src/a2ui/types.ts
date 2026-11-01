/** A2UI protocol types understood by the OpenNeko renderer. */

export type A2UIVersion = "v0.9" | "v1.0";

/** A literal value or a JSON Pointer reference into the surface data model. */
export type DynamicValue<T> = T | { path: string };

export interface A2UIComponent {
  id: string;
  component: string;
  [key: string]: unknown;
}

export interface CreateSurfaceMessage {
  version: A2UIVersion;
  createSurface: {
    surfaceId: string;
    catalogId: string;
    /** v0.9 compatibility. New surfaces use surfaceProperties. */
    theme?: Record<string, unknown>;
    surfaceProperties?: Record<string, unknown>;
    sendDataModel?: boolean;
    /** v1.0 can initialize a complete surface in one message. */
    components?: A2UIComponent[];
    dataModel?: Record<string, unknown>;
  };
}

export interface UpdateComponentsMessage {
  version: A2UIVersion;
  updateComponents: {
    surfaceId: string;
    components: A2UIComponent[];
  };
}

export interface UpdateDataModelMessage {
  version: A2UIVersion;
  updateDataModel: {
    surfaceId: string;
    path?: string;
    value?: unknown;
  };
}

export interface DeleteSurfaceMessage {
  version: A2UIVersion;
  deleteSurface: { surfaceId: string };
}

export type A2UIMessage =
  | CreateSurfaceMessage
  | UpdateComponentsMessage
  | UpdateDataModelMessage
  | DeleteSurfaceMessage;

export interface SurfaceState {
  version: A2UIVersion;
  surfaceId: string;
  catalogId: string;
  components: Map<string, A2UIComponent>;
  dataModel: Record<string, unknown>;
  theme?: Record<string, unknown>;
  surfaceProperties?: Record<string, unknown>;
  sendDataModel?: boolean;
}

export interface A2UIAction {
  version: A2UIVersion;
  action: {
    name: string;
    surfaceId: string;
    sourceComponentId: string;
    timestamp: string;
    context: Record<string, unknown>;
    wantResponse?: boolean;
    actionId?: string;
  };
}
