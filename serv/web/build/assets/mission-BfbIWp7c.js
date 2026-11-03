import{C as e,tt as t}from"./index-FMdl0QJJ.js";var n=t(`book-open-check`,[[`path`,{d:`M12 21V7`,key:`gj6g52`}],[`path`,{d:`m16 12 2 2 4-4`,key:`mdajum`}],[`path`,{d:`M22 6V4a1 1 0 0 0-1-1h-5a4 4 0 0 0-4 4 4 4 0 0 0-4-4H3a1 1 0 0 0-1 1v13a1 1 0 0 0 1 1h6a3 3 0 0 1 3 3 3 3 0 0 1 3-3h6a1 1 0 0 0 1-1v-1.3`,key:`8arnkb`}]]),r=`query MissionTasks {
  gj_task(order_by: { updated_at: desc }, limit: 100) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,i=`query MissionTask($id: String!) {
  gj_task(where: { id: { eq: $id } }, limit: 1) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,a=`query MissionTaskEntries($task_id: String!) {
  gj_task_entry(
    where: { task_id: { eq: $task_id } }
    order_by: { created_at: desc }
    limit: 100
  ) {
    id task_id origin body detail_json status trace_id watch_id owner_ref
    created_at updated_at
  }
}`,o=`query MissionWatches {
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
}`,s=`query MissionAnnotations {
  gj_artifacts(
    where: { kind: { eq: "annotation" } }
    order_by: { updated_at: desc }
    limit: 100
  ) {
    id kind target_ref content tier catalog_revision task_id author_ref approved_ref
    approved_at revision created_at updated_at
  }
}`,c=`id kind target_ref content tier catalog_revision task_id author_ref
  approved_ref approved_at revision created_at updated_at`;async function l(){return v(await e(r),`gj_task`)}async function u(t){return v(await e(i,{id:t}),`gj_task`)[0]||null}async function d(t){return v(await e(a,{task_id:t}),`gj_task_entry`)}async function f(){let t=await e(o);return{watches:t?.data?.gj_watch||[],events:t?.data?.gj_watch_event||[]}}async function p(){return v(await e(s),`gj_artifacts`)}async function m(t){return(await e(`mutation MissionMarkWatchEventSeen($id: String!) {
      gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) {
        id seen seen_at updated_at
      }
    }`,{id:t}))?.data?.gj_watch_event}async function h(t){return(await e(`mutation MissionApproveAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) {
        ${c}
      }
    }`,{id:t}))?.data?.gj_artifacts}async function g(t){return(await e(`mutation MissionDemoteAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) {
        ${c}
      }
    }`,{id:t}))?.data?.gj_artifacts}function _(e){return e?.kind===`graphql`&&/\b(unknown|unsupported|disabled|not (?:available|configured|found)|cannot query)\b/i.test(e.message||``)}function v(e,t){return e?.data?.[t]||[]}var y=`id name query schedule next_fire_at metric metric_state delivery_json status enabled last_error last_fired_at`;async function b({id:t,name:n,query:r,schedule:i,metric:a,prompt:o}){let s={name:n,query:r,schedule:i,metric:a,delivery_json:o?.trim()?{kind:`agent`,prompt:o.trim()}:{kind:`inbox`}};return w((t?await e(`mutation MissionUpdateMetricWatch($id: String!, $input: gj_watchInput!) {
          gj_watch(where: { id: { eq: $id } }, update: $input) { ${y} }
        }`,{id:t,input:s}):await e(`mutation MissionCreateMetricWatch($input: gj_watchInput!) {
          gj_watch(insert: $input) { ${y} }
        }`,{input:s}))?.data?.gj_watch)}async function x(t,n){return w((await e(`mutation MissionSetWatchEnabled($id: String!, $enabled: Boolean!) {
      gj_watch(where: { id: { eq: $id } }, update: { enabled: $enabled }) { id enabled status }
    }`,{id:t,enabled:n}))?.data?.gj_watch)}async function S(t){await e(`mutation MissionDeleteWatch($id: String!) {
      gj_watch(delete: true, where: { id: { eq: $id } }) { id }
    }`,{id:t})}async function C(t){return(await e(t))?.data??null}function w(e){return Array.isArray(e)?e[0]||null:e||null}export{u as a,f as c,C as d,b as f,p as i,_ as l,n as m,S as n,d as o,x as p,g as r,l as s,h as t,m as u};