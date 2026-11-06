import{D as e}from"./index-DAmNIm0m.js";var t=`query MissionTasks {
  gj_task(order_by: { updated_at: desc }, limit: 100) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,n=`query MissionTask($id: String!) {
  gj_task(where: { id: { eq: $id } }, limit: 1) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,r=`query MissionTaskEntries($task_id: String!) {
  gj_task_entry(
    where: { task_id: { eq: $task_id } }
    order_by: { created_at: desc }
    limit: 100
  ) {
    id task_id origin body detail_json status trace_id watch_id owner_ref
    created_at updated_at
  }
}`,i=`query MissionWatches {
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
}`,a=`query MissionAnnotations {
  gj_artifacts(
    where: { kind: { eq: "annotation" } }
    order_by: { updated_at: desc }
    limit: 100
  ) {
    id kind target_ref content tier catalog_revision task_id author_ref approved_ref
    approved_at revision created_at updated_at
  }
}`,o=`id kind target_ref content tier catalog_revision task_id author_ref
  approved_ref approved_at revision created_at updated_at`;async function s(){return g(await e(t),`gj_task`)}async function c(t){return g(await e(n,{id:t}),`gj_task`)[0]||null}async function l(t){return g(await e(r,{task_id:t}),`gj_task_entry`)}async function u(){let t=await e(i);return{watches:t?.data?.gj_watch||[],events:t?.data?.gj_watch_event||[]}}async function d(){return g(await e(a),`gj_artifacts`)}async function f(t){return(await e(`mutation MissionMarkWatchEventSeen($id: String!) {
      gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) {
        id seen seen_at updated_at
      }
    }`,{id:t}))?.data?.gj_watch_event}async function p(t){return(await e(`mutation MissionApproveAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) {
        ${o}
      }
    }`,{id:t}))?.data?.gj_artifacts}async function m(t){return(await e(`mutation MissionDemoteAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) {
        ${o}
      }
    }`,{id:t}))?.data?.gj_artifacts}function h(e){return e?.kind===`graphql`&&/\b(unknown|unsupported|disabled|not (?:available|configured|found)|cannot query)\b/i.test(e.message||``)}function g(e,t){return e?.data?.[t]||[]}var _=`id name query schedule next_fire_at metric metric_state delivery_json status enabled last_error last_fired_at`;async function v({id:t,name:n,query:r,schedule:i,metric:a,prompt:o,profile:s}){let c={kind:`inbox`};(o?.trim()||s)&&(c={kind:`agent`},o?.trim()&&(c.prompt=o.trim()),s&&(c.profile=s));let l={name:n,query:r,schedule:i,metric:a,delivery_json:c};return S((t?await e(`mutation MissionUpdateMetricWatch($id: String!, $input: gj_watchInput!) {
          gj_watch(where: { id: { eq: $id } }, update: $input) { ${_} }
        }`,{id:t,input:l}):await e(`mutation MissionCreateMetricWatch($input: gj_watchInput!) {
          gj_watch(insert: $input) { ${_} }
        }`,{input:l}))?.data?.gj_watch)}async function y(t,n){return S((await e(`mutation MissionSetWatchEnabled($id: String!, $enabled: Boolean!) {
      gj_watch(where: { id: { eq: $id } }, update: { enabled: $enabled }) { id enabled status }
    }`,{id:t,enabled:n}))?.data?.gj_watch)}async function b(t){await e(`mutation MissionDeleteWatch($id: String!) {
      gj_watch(delete: true, where: { id: { eq: $id } }) { id }
    }`,{id:t})}async function x(t){return(await e(t))?.data??null}function S(e){return Array.isArray(e)?e[0]||null:e||null}export{c as a,u as c,x as d,v as f,d as i,h as l,b as n,l as o,y as p,m as r,s,p as t,f as u};