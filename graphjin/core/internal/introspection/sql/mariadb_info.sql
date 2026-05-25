SELECT
    (CAST(SUBSTRING_INDEX(v, '.', 1) AS SIGNED INTEGER) * 10000 +
     CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(v, '.', 2), '.', -1) AS SIGNED INTEGER) * 100 +
     CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(v, '.', 3), '.', -1) AS SIGNED INTEGER)) as db_version,
    db as db_schema,
    db as db_name
FROM (SELECT SUBSTRING_INDEX(VERSION(), '-', 1) as v, DATABASE() as db) as x;
