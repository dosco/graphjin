create table {{.prefix}}_bar(id serial primary key);

---- create above / drop below ----

drop table {{.prefix}}_bar;
