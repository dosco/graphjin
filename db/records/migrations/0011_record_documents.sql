-- Records keep their fields in one jsonb document column named data. The
-- audit trigger flattens that document before it compares rows, so the
-- change log still lists one entry per changed field.

create or replace function engine.capture_record_change()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog, engine
as $$
declare
  -- Fields live in the data document. Flattening it into the row keeps the
  -- change log, recycle names and import checks keyed by field api names.
  new_row jsonb := (to_jsonb(new) - 'data') || coalesce(to_jsonb(new)->'data', '{}'::jsonb);
  old_row jsonb := case
    when tg_op = 'UPDATE'
      then (to_jsonb(old) - 'data') || coalesce(to_jsonb(old)->'data', '{}'::jsonb)
    else '{}'::jsonb
  end;
  v_action_kind text;
  v_action_context jsonb;
  v_change_action text;
  v_actor_user_id text;
  v_action_request_id text;
  v_mutation_id text;
  v_changes jsonb;
  v_name_field text;
  v_visibility text;
  v_import_run_id uuid;
  v_import_object_api_name text;
  v_import_batch_no integer;
  v_import_batch_hash text;
begin
  if tg_nargs <> 2 or tg_argv[0] = '' or tg_argv[1] = '' then
    raise exception 'records audit trigger requires app and object identifiers';
  end if;

  if new_row->>'org_id' is null or new_row->>'id' is null then
    raise exception 'records audit trigger requires org_id and id';
  end if;

  v_action_request_id := new_row->>'nk_action_request_id';
  v_mutation_id := new_row->>'nk_mutation_id';
  v_actor_user_id := coalesce(new_row->>'nk_updated_by', new_row->>'nk_created_by');
  if v_action_request_id is null or v_action_request_id = '' then
    raise exception 'records mutation requires nk_action_request_id';
  end if;
  if v_mutation_id is null or v_mutation_id = '' then
    raise exception 'records mutation requires nk_mutation_id';
  end if;

  select ae.action_kind, ae.result into v_action_kind, v_action_context
  from engine.action_execution ae
  where ae.action_request_id = v_action_request_id
    and ae.status in ('claimed', 'running');
  if v_action_kind is null then
    raise exception 'records mutation requires a live action execution claim';
  end if;

  if tg_op = 'INSERT' then
    v_change_action := case
      when v_action_kind in ('record_import', 'app_import', 'records_import_start') then 'import'
      when v_action_kind in ('record_sync', 'app_sync') then 'sync'
      else 'create'
    end;
  elsif old_row->>'nk_deleted_at' is null
      and new_row->>'nk_deleted_at' is not null then
    v_change_action := 'delete';
  elsif old_row->>'nk_deleted_at' is not null
      and new_row->>'nk_deleted_at' is null then
    v_change_action := 'restore';
  else
    v_change_action := case
      when v_action_kind in ('record_import', 'app_import', 'records_import_start') then 'import'
      when v_action_kind in ('record_sync', 'app_sync') then 'sync'
      else 'update'
    end;
  end if;

  select coalesce(
    jsonb_object_agg(
      keys.key,
      jsonb_build_object('old', old_row->keys.key, 'new', new_row->keys.key)
      order by keys.key
    ),
    '{}'::jsonb
  ) into v_changes
  from (
    select key from jsonb_object_keys(old_row || new_row) as key
    where key <> 'org_id'
      and key not like 'nk\_%' escape '\'
      and old_row->key is distinct from new_row->key
  ) as keys;

  insert into engine.record_change_log (
    org_id,
    app_id,
    object_api_name,
    record_id,
    action,
    actor_user_id,
    action_request_id,
    mutation_id,
    changes
  ) values (
    new_row->>'org_id',
    tg_argv[0],
    tg_argv[1],
    new_row->>'id',
    v_change_action,
    v_actor_user_id,
    v_action_request_id,
    v_mutation_id,
    v_changes
  ) on conflict (mutation_id) do nothing;

  if v_change_action in ('delete', 'restore') then
    select ro.name_field, ro.visibility into v_name_field, v_visibility
    from engine.record_object ro
    where ro.org_id = new_row->>'org_id'
      and ro.app_id = tg_argv[0]
      and ro.api_name = tg_argv[1];
    if v_name_field is null then
      raise exception 'records recycle mutation requires a registered object';
    end if;
  end if;

  if v_change_action = 'delete' then
    insert into engine.recycle_record (
      org_id,
      app_id,
      object_api_name,
      visibility,
      record_id,
      record_name,
      owner_user_id,
      deleted_at,
      deleted_by,
      deletion_action_request_id
    ) values (
      new_row->>'org_id',
      tg_argv[0],
      tg_argv[1],
      v_visibility,
      new_row->>'id',
      coalesce(new_row->>v_name_field, new_row->>'id'),
      new_row->>'owner_user_id',
      (new_row->>'nk_deleted_at')::timestamptz,
      v_actor_user_id,
      v_action_request_id
    )
    on conflict (org_id, app_id, object_api_name, record_id) do update
      set visibility = excluded.visibility,
          record_name = excluded.record_name,
          owner_user_id = excluded.owner_user_id,
          deleted_at = excluded.deleted_at,
          deleted_by = excluded.deleted_by,
          deletion_action_request_id = excluded.deletion_action_request_id;
  elsif v_change_action = 'restore' then
    delete from engine.recycle_record
    where org_id = new_row->>'org_id'
      and app_id = tg_argv[0]
      and object_api_name = tg_argv[1]
      and record_id = new_row->>'id';
  end if;

  if v_action_kind in ('record_import', 'app_import', 'records_import_start') then
    begin
      v_import_run_id := (v_action_context #>> '{import_batch,run_id}')::uuid;
      v_import_object_api_name := v_action_context #>> '{import_batch,object_api_name}';
      v_import_batch_no := (v_action_context #>> '{import_batch,batch_no}')::integer;
      v_import_batch_hash := v_action_context #>> '{import_batch,batch_hash}';
    exception when others then
      raise exception 'records import mutation has invalid batch context';
    end;
    if v_import_run_id is null
       or v_import_object_api_name is null
       or v_import_object_api_name <> tg_argv[1]
       or v_import_batch_no is null
       or v_import_batch_no < 0
       or v_import_batch_hash is null
       or v_import_batch_hash = '' then
      raise exception 'records import mutation requires complete matching batch context';
    end if;

    insert into engine.import_batch_receipt (
      import_run_id, object_api_name, batch_no, batch_hash, row_count
    ) values (
      v_import_run_id, v_import_object_api_name, v_import_batch_no,
      v_import_batch_hash, 1
    )
    on conflict (import_run_id, object_api_name, batch_no) do update
      set row_count = engine.import_batch_receipt.row_count + 1
      where engine.import_batch_receipt.batch_hash = excluded.batch_hash;

    if not found then
      raise exception 'records import batch receipt identity mismatch';
    end if;
  end if;

  return new;
end;
$$;

insert into engine.substrate_version (name, version)
values ('record_audit_trigger', 5)
on conflict (name) do update
set version = excluded.version,
    updated_at = now();
