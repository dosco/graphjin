import{R as e,it as t}from"./index-veRmFzUK.js";var n=t(`book-open-check`,[[`path`,{d:`M12 21V7`,key:`gj6g52`}],[`path`,{d:`m16 12 2 2 4-4`,key:`mdajum`}],[`path`,{d:`M22 6V4a1 1 0 0 0-1-1h-5a4 4 0 0 0-4 4 4 4 0 0 0-4-4H3a1 1 0 0 0-1 1v13a1 1 0 0 0 1 1h6a3 3 0 0 1 3 3 3 3 0 0 1 3-3h6a1 1 0 0 0 1-1v-1.3`,key:`8arnkb`}]]),r=t(`plus`,[[`path`,{d:`M5 12h14`,key:`1ays0h`}],[`path`,{d:`M12 5v14`,key:`s699le`}]]),i=`query MissionTasks {
  gj_task(order_by: { updated_at: desc }, limit: 100) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,a=`query MissionTask($id: String!) {
  gj_task(where: { id: { eq: $id } }, limit: 1) {
    id goal status outcome snapshot_json verify_json verify_status verify_after
    verify_attempts owner_ref account_ref last_entry_at created_at updated_at closed_at
  }
}`,o=`query MissionTaskEntries($task_id: String!) {
  gj_task_entry(
    where: { task_id: { eq: $task_id } }
    order_by: { created_at: desc }
    limit: 100
  ) {
    id task_id origin body detail_json status trace_id watch_id owner_ref
    created_at updated_at
  }
}`,s=`query MissionWatches {
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
}`,c=`query MissionAnnotations {
  gj_artifacts(
    where: { kind: { eq: "annotation" } }
    order_by: { updated_at: desc }
    limit: 100
  ) {
    id kind target_ref content tier catalog_revision task_id author_ref approved_ref
    approved_at revision created_at updated_at
  }
}`,l=`id kind target_ref content tier catalog_revision task_id author_ref
  approved_ref approved_at revision created_at updated_at`;async function u(){return y(await e(i),`gj_task`)}async function d(t){return y(await e(a,{id:t}),`gj_task`)[0]||null}async function f(t){return y(await e(o,{task_id:t}),`gj_task_entry`)}async function p(){let t=await e(s);return{watches:t?.data?.gj_watch||[],events:t?.data?.gj_watch_event||[]}}async function m(){return y(await e(c),`gj_artifacts`)}async function h(t){return(await e(`mutation MissionMarkWatchEventSeen($id: String!) {
      gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) {
        id seen seen_at updated_at
      }
    }`,{id:t}))?.data?.gj_watch_event}async function g(t){return(await e(`mutation MissionApproveAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) {
        ${l}
      }
    }`,{id:t}))?.data?.gj_artifacts}async function _(t){return(await e(`mutation MissionDemoteAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) {
        ${l}
      }
    }`,{id:t}))?.data?.gj_artifacts}function v(e){return e?.kind===`graphql`&&/\b(unknown|unsupported|disabled|not (?:available|configured|found)|cannot query)\b/i.test(e.message||``)}function y(e,t){return e?.data?.[t]||[]}var b=`id name query schedule next_fire_at metric metric_state delivery_json status enabled last_error last_fired_at`;async function x({id:t,name:n,query:r,schedule:i,metric:a,prompt:o,profile:s}){let c={kind:`inbox`};(o?.trim()||s)&&(c={kind:`agent`},o?.trim()&&(c.prompt=o.trim()),s&&(c.profile=s));let l={name:n,query:r,schedule:i,metric:a,delivery_json:c};return T((t?await e(`mutation MissionUpdateMetricWatch($id: String!, $input: gj_watchInput!) {
          gj_watch(where: { id: { eq: $id } }, update: $input) { ${b} }
        }`,{id:t,input:l}):await e(`mutation MissionCreateMetricWatch($input: gj_watchInput!) {
          gj_watch(insert: $input) { ${b} }
        }`,{input:l}))?.data?.gj_watch)}async function S(t,n){return T((await e(`mutation MissionSetWatchEnabled($id: String!, $enabled: Boolean!) {
      gj_watch(where: { id: { eq: $id } }, update: { enabled: $enabled }) { id enabled status }
    }`,{id:t,enabled:n}))?.data?.gj_watch)}async function C(t){await e(`mutation MissionDeleteWatch($id: String!) {
      gj_watch(delete: true, where: { id: { eq: $id } }) { id }
    }`,{id:t})}async function w(t){return(await e(t))?.data??null}function T(e){return Array.isArray(e)?e[0]||null:e||null}export{d as a,p as c,w as d,x as f,n as h,m as i,v as l,r as m,C as n,f as o,S as p,_ as r,u as s,g as t,h as u};