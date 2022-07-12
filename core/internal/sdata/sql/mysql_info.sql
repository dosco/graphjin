SELECT 
		a.c as db_version, 
    b.c as db_schema, 
    b.c as db_name
FROM 
	(SELECT CONVERT(REPLACE(VERSION(), '.', ''), SIGNED INTEGER) as c) as a, 
    (SELECT DATABASE() as c) as b;