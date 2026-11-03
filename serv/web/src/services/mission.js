import { graphqlRequest } from "./graphql";

const tasksQuery = `query MissionTasks {
  gj_task(order_by: { updated_at: desc }, limit: 100) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`;

const taskQuery = `query MissionTask($id: String!) {
  gj_task(where: { id: { eq: $id } }, limit: 1) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`;

const taskEntriesQuery = `query MissionTaskEntries($task_id: String!) {
  gj_task_entry(
    where: { task_id: { eq: $task_id } }
    order_by: { created_at: desc }
    limit: 100
  ) {
    id task_id origin body detail_json status trace_id watch_id owner_ref
    created_at updated_at
  }
}`;

const watchesQuery = `query MissionWatches {
  gj_watch(order_by: { updated_at: desc }, limit: 100) {
    id name task_id description query lifecycle status approval enabled last_fired_at
    last_error failure_count delivery_json schedule next_fire_at metric metric_state
    routine_thread_id created_at updated_at
  }
  gj_watch_event(
    where: { seen: { eq: false } }
    order_by: { created_at: desc }
    limit: 100
  ) {
    id watch_id data_hash data_json data_truncated evidence_json delivery_status
    delivery_attempts delivery_json receipt_json enrichment_json seen seen_at
    snoozed_until created_at updated_at
  }
}`;

const annotationsQuery = `query MissionAnnotations {
  gj_artifacts(
    where: { kind: { eq: "annotation" } }
    order_by: { updated_at: desc }
    limit: 100
  ) {
    id kind target_ref content tier catalog_revision task_id author_ref approved_ref
    approved_at revision created_at updated_at
  }
}`;

const annotationFields = `id kind target_ref content tier catalog_revision task_id author_ref
  approved_ref approved_at revision created_at updated_at`;

export async function fetchMissionTasks() {
  return dataRows(await graphqlRequest(tasksQuery), "gj_task");
}

export async function fetchMissionTask(id) {
  const rows = dataRows(await graphqlRequest(taskQuery, { id }), "gj_task");
  return rows[0] || null;
}

export async function fetchMissionTaskEntries(taskID) {
  return dataRows(await graphqlRequest(taskEntriesQuery, { task_id: taskID }), "gj_task_entry");
}

export async function fetchMissionWatches() {
  const payload = await graphqlRequest(watchesQuery);
  return {
    watches: payload?.data?.gj_watch || [],
    events: payload?.data?.gj_watch_event || [],
  };
}

export async function fetchMissionAnnotations() {
  return dataRows(await graphqlRequest(annotationsQuery), "gj_artifacts");
}

export async function markWatchEventSeen(id) {
  const payload = await graphqlRequest(
    `mutation MissionMarkWatchEventSeen($id: String!) {
      gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) {
        id seen seen_at updated_at
      }
    }`,
    { id }
  );
  return payload?.data?.gj_watch_event;
}

export async function approveAnnotation(id) {
  const payload = await graphqlRequest(
    `mutation MissionApproveAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) {
        ${annotationFields}
      }
    }`,
    { id }
  );
  return payload?.data?.gj_artifacts;
}

export async function demoteAnnotation(id) {
  const payload = await graphqlRequest(
    `mutation MissionDemoteAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) {
        ${annotationFields}
      }
    }`,
    { id }
  );
  return payload?.data?.gj_artifacts;
}

export function isFeatureUnavailable(error) {
  return error?.kind === "graphql" && /\b(unknown|unsupported|disabled|not (?:available|configured|found)|cannot query)\b/i.test(error.message || "");
}

function dataRows(payload, root) {
  return payload?.data?.[root] || [];
}

const watchFields = `id name query schedule next_fire_at metric metric_state delivery_json status enabled last_error last_fired_at`;

// saveMetricWatch creates or replaces a metric watch: a schedule, a plain
// query, and the threshold its number is checked against on every tick.
// Watches are keyed by owner and name, so saving an existing name updates it.
export async function saveMetricWatch({ id, name, query, schedule, metric, prompt }) {
  const delivery = prompt?.trim() ? { kind: "agent", prompt: prompt.trim() } : { kind: "inbox" };
  const input = { name, query, schedule, metric, delivery_json: delivery };
  const payload = id
    ? await graphqlRequest(
        `mutation MissionUpdateMetricWatch($id: String!, $input: gj_watchInput!) {
          gj_watch(where: { id: { eq: $id } }, update: $input) { ${watchFields} }
        }`,
        { id, input }
      )
    : await graphqlRequest(
        `mutation MissionCreateMetricWatch($input: gj_watchInput!) {
          gj_watch(insert: $input) { ${watchFields} }
        }`,
        { input }
      );
  return firstRow(payload?.data?.gj_watch);
}

export async function setWatchEnabled(id, enabled) {
  const payload = await graphqlRequest(
    `mutation MissionSetWatchEnabled($id: String!, $enabled: Boolean!) {
      gj_watch(where: { id: { eq: $id } }, update: { enabled: $enabled }) { id enabled status }
    }`,
    { id, enabled }
  );
  return firstRow(payload?.data?.gj_watch);
}

export async function deleteWatch(id) {
  await graphqlRequest(
    `mutation MissionDeleteWatch($id: String!) {
      gj_watch(delete: true, where: { id: { eq: $id } }) { id }
    }`,
    { id }
  );
}

// previewQuery runs a read once so the editor can offer the numbers in its
// result as metric paths.
export async function previewQuery(query) {
  const payload = await graphqlRequest(query);
  return payload?.data ?? null;
}

function firstRow(value) {
  return Array.isArray(value) ? value[0] || null : value || null;
}
