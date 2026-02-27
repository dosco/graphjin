SELECT '' AS func_id,
	'' AS func_schema,
	'' AS func_name,
	'' AS data_type,
	CAST(NULL AS NUMBER) AS param_id,
	'' AS param_name,
	'' AS param_type,
	'' AS param_kind
FROM information_schema.functions
WHERE 1 = 0;
