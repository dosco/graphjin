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
