-- Scalekit and magic-link sign-in moved from plugins into core. Rows keyed by
-- the plugin package name now use the built-in provider id, so IdP groups,
-- directory-synced users and sync state keep matching after the upgrade.

update sso_group set provider = case provider
  when '@open-neko/plugin-scalekit' then 'scalekit'
  when '@open-neko/plugin-magic-link' then 'magic-link'
end
where provider in ('@open-neko/plugin-scalekit', '@open-neko/plugin-magic-link');

update sso_group_sync_audit set provider = case provider
  when '@open-neko/plugin-scalekit' then 'scalekit'
  when '@open-neko/plugin-magic-link' then 'magic-link'
end
where provider in ('@open-neko/plugin-scalekit', '@open-neko/plugin-magic-link');

update directory_sync_state set provider = case provider
  when '@open-neko/plugin-scalekit' then 'scalekit'
  when '@open-neko/plugin-magic-link' then 'magic-link'
end
where provider in ('@open-neko/plugin-scalekit', '@open-neko/plugin-magic-link');

update app_user set source = case source
  when '@open-neko/plugin-scalekit' then 'scalekit'
  when '@open-neko/plugin-magic-link' then 'magic-link'
end
where source in ('@open-neko/plugin-scalekit', '@open-neko/plugin-magic-link');

-- The Scalekit workspace connection is an integration item keyed by its id.
update item_grant g set item_id = 'scalekit'
where g.item_type = 'integration'
  and g.item_id = '@open-neko/plugin-scalekit'
  and not exists (
    select 1 from item_grant n
    where n.org_id = g.org_id and n.group_id = g.group_id
      and n.item_type = 'integration' and n.item_id = 'scalekit'
  );
