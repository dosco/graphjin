import { fetchJSON } from "./graphql";

export async function fetchConsoleBootstrap() {
  try {
    return await fetchJSON("/api/v1/console/bootstrap");
  } catch (error) {
    if (error?.status === 404) {
      return {
        schema_version: "1",
        workspaces: [
          { id: "user", default_path: "/user/agent", capabilities: [] },
          { id: "admin", default_path: "/admin/runtime", capabilities: [] },
        ],
        features: [],
      };
    }
    throw error;
  }
}
