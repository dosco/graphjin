-- Slack, Telegram and the HUD channel moved from plugins into core. Rows keyed
-- by the plugin package name now use the built-in channel id. The column keeps
-- its name (channel_plugin) so nothing else changes shape.

update delivery_binding set channel_plugin = case channel_plugin
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where channel_plugin in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud');

update channel_workspace set channel_plugin = case channel_plugin
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where channel_plugin in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud');

update channel_identity set channel_plugin = case channel_plugin
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where channel_plugin in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud');

update channel_poll_cursor set channel_plugin = case channel_plugin
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where channel_plugin in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud');

update inbound_dedup set channel_plugin = case channel_plugin
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where channel_plugin in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud');

-- Channel item grants follow the id; a group that already holds the new id keeps it.
update item_grant g set item_id = case g.item_id
  when '@open-neko/plugin-slack' then 'slack'
  when '@open-neko/channel-telegram' then 'telegram'
  when '@open-neko/channel-hud' then 'hud'
end
where g.item_type = 'channel'
  and g.item_id in ('@open-neko/plugin-slack', '@open-neko/channel-telegram', '@open-neko/channel-hud')
  and not exists (
    select 1 from item_grant n
    where n.org_id = g.org_id and n.group_id = g.group_id and n.item_type = 'channel'
      and n.item_id = case g.item_id
        when '@open-neko/plugin-slack' then 'slack'
        when '@open-neko/channel-telegram' then 'telegram'
        when '@open-neko/channel-hud' then 'hud'
      end
  );
