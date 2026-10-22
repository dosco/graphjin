-- The in-stack GraphJin and agent runtime service is renamed from `graphjin`
-- to `openneko-serve`, so its Docker hostname changes. Point data sources
-- that used the old in-stack hostname at the new one.
update data_source
set graphql_url = 'http://openneko-serve:8080' || substr(graphql_url, length('http://graphjin:8080') + 1)
where graphql_url = 'http://graphjin:8080'
   or graphql_url like 'http://graphjin:8080/%';
