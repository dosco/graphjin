import{Y as e,x as t}from"./index-DLCeOHRX.js";var n=e(`book-open-check`,[[`path`,{d:`M12 21V7`,key:`gj6g52`}],[`path`,{d:`m16 12 2 2 4-4`,key:`mdajum`}],[`path`,{d:`M22 6V4a1 1 0 0 0-1-1h-5a4 4 0 0 0-4 4 4 4 0 0 0-4-4H3a1 1 0 0 0-1 1v13a1 1 0 0 0 1 1h6a3 3 0 0 1 3 3 3 3 0 0 1 3-3h6a1 1 0 0 0 1-1v-1.3`,key:`8arnkb`}]]),r=`query MissionTasks {
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
    id name task_id description lifecycle status approval enabled last_fired_at
    last_error failure_count delivery_json created_at updated_at
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
  approved_ref approved_at revision created_at updated_at`;async function l(){return v(await t(r),`gj_task`)}async function u(e){return v(await t(i,{id:e}),`gj_task`)[0]||null}async function d(e){return v(await t(a,{task_id:e}),`gj_task_entry`)}async function f(){let e=await t(o);return{watches:e?.data?.gj_watch||[],events:e?.data?.gj_watch_event||[]}}async function p(){return v(await t(s),`gj_artifacts`)}async function m(e){return(await t(`mutation MissionMarkWatchEventSeen($id: String!) {
      gj_watch_event(where: { id: { eq: $id } }, update: { seen: true }) {
        id seen seen_at updated_at
      }
    }`,{id:e}))?.data?.gj_watch_event}async function h(e){return(await t(`mutation MissionApproveAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "approved" }) {
        ${c}
      }
    }`,{id:e}))?.data?.gj_artifacts}async function g(e){return(await t(`mutation MissionDemoteAnnotation($id: String!) {
      gj_artifacts(where: { id: { eq: $id } }, update: { tier: "observed" }) {
        ${c}
      }
    }`,{id:e}))?.data?.gj_artifacts}function _(e){return e?.kind===`graphql`&&/\b(unknown|unsupported|disabled|not (?:available|configured|found)|cannot query)\b/i.test(e.message||``)}function v(e,t){return e?.data?.[t]||[]}export{d as a,_ as c,u as i,m as l,g as n,l as o,p as r,f as s,h as t,n as u};