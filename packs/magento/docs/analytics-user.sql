-- Run as a MariaDB/MySQL administrator after replacing the placeholders.
-- Do not provide the administrator credential to OpenNeko.
CREATE USER IF NOT EXISTS 'magento_analytics'@'<GRAPHJIN_NETWORK_HOST>'
  IDENTIFIED BY '<GENERATE_A_UNIQUE_PASSWORD>';

GRANT SELECT ON `<MAGENTO_DATABASE>`.*
  TO 'magento_analytics'@'<GRAPHJIN_NETWORK_HOST>';

-- OpenNeko doctor verifies that this account cannot create or mutate data.
-- For stricter deployments, replace the database-wide SELECT grant with
-- explicit SELECT grants for only the tables listed in graphjin/sources.yaml.
SHOW GRANTS FOR 'magento_analytics'@'<GRAPHJIN_NETWORK_HOST>';
