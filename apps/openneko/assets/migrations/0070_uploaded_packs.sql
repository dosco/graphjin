-- Uploaded bundles live immutably in the organization workspace. The installed
-- version/content identity is pinned in pack_install.config._bundle.
alter table pack_install drop constraint if exists pack_install_source_check;
alter table pack_install add constraint pack_install_source_check
  check (source in ('embedded', 'uploaded'));
