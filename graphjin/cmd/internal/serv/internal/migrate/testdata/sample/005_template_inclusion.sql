{{ template "shared/v1_001.sql" . }}

---- create above / drop below ----

drop view {{.prefix}}v1;
