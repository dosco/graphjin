CREATE TABLE roast_batches (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  batch_code STRING NOT NULL,
  profile_id INT64 NOT NULL,
  green_lot_id INT64 NOT NULL,
  machine_id STRING NOT NULL,
  started_at TIMESTAMP NOT NULL,
  ended_at TIMESTAMP NOT NULL,
  target_temp_c NUMERIC,
  final_temp_c NUMERIC,
  development_seconds INT64,
  drop_reason STRING,
  yield_kg NUMERIC
);

CREATE TABLE roast_sensor_samples (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  batch_id INT64 NOT NULL REFERENCES roast_batches(id) NOT ENFORCED,
  recorded_at TIMESTAMP NOT NULL,
  bean_temp_c NUMERIC,
  env_temp_c NUMERIC,
  ror_c_per_min NUMERIC,
  gas_percent NUMERIC,
  airflow_percent NUMERIC,
  drum_rpm INT64
);

CREATE TABLE qc_cupping_scores (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  batch_id INT64 NOT NULL REFERENCES roast_batches(id) NOT ENFORCED,
  scored_at TIMESTAMP NOT NULL,
  fragrance NUMERIC,
  flavor NUMERIC,
  acidity NUMERIC,
  body NUMERIC,
  balance NUMERIC,
  defects INT64,
  total_score NUMERIC,
  notes STRING
);

CREATE TABLE equipment_telemetry (
  id INT64 NOT NULL PRIMARY KEY NOT ENFORCED,
  machine_id STRING NOT NULL,
  recorded_at TIMESTAMP NOT NULL,
  vibration_mm_s NUMERIC,
  exhaust_pa NUMERIC,
  burner_duty_percent NUMERIC
);

INSERT INTO roast_batches (id, batch_code, profile_id, green_lot_id, machine_id, started_at, ended_at, target_temp_c, final_temp_c, development_seconds, drop_reason, yield_kg) VALUES
  (1001, 'RB-2026-0605-001', 1, 1, 'loring-s35', TIMESTAMP '2026-06-05 14:00:00', TIMESTAMP '2026-06-05 14:13:30', 214.50, 214.70, 108, 'target met', 58.40),
  (1002, 'RB-2026-0605-002', 2, 2, 'probats-p25', TIMESTAMP '2026-06-05 16:00:00', TIMESTAMP '2026-06-05 16:11:40', 211.00, 210.60, 88, 'operator drop', 23.80),
  (1003, 'RB-2026-0605-003', 3, 3, 'loring-s35', TIMESTAMP '2026-06-05 18:00:00', TIMESTAMP '2026-06-05 18:12:10', 212.25, 212.10, 96, 'target met', 39.60);

INSERT INTO roast_sensor_samples (id, batch_id, recorded_at, bean_temp_c, env_temp_c, ror_c_per_min, gas_percent, airflow_percent, drum_rpm) VALUES
  (1, 1001, TIMESTAMP '2026-06-05 14:03:00', 151.20, 218.00, 12.40, 72.00, 35.00, 58),
  (2, 1001, TIMESTAMP '2026-06-05 14:07:00', 188.60, 226.30, 8.20, 62.00, 45.00, 58),
  (3, 1001, TIMESTAMP '2026-06-05 14:11:00', 207.80, 220.10, 4.10, 41.00, 61.00, 58),
  (4, 1002, TIMESTAMP '2026-06-05 16:03:00', 148.90, 214.80, 13.10, 75.00, 34.00, 54),
  (5, 1002, TIMESTAMP '2026-06-05 16:07:00', 190.10, 224.60, 9.80, 68.00, 41.00, 54),
  (6, 1002, TIMESTAMP '2026-06-05 16:10:30', 207.20, 221.00, 5.90, 53.00, 56.00, 54),
  (7, 1003, TIMESTAMP '2026-06-05 18:03:00', 150.70, 217.10, 12.80, 70.00, 36.00, 58),
  (8, 1003, TIMESTAMP '2026-06-05 18:07:00', 187.40, 223.40, 8.40, 60.00, 44.00, 58),
  (9, 1003, TIMESTAMP '2026-06-05 18:11:00', 207.90, 220.20, 4.60, 44.00, 60.00, 58);

INSERT INTO qc_cupping_scores (id, batch_id, scored_at, fragrance, flavor, acidity, body, balance, defects, total_score, notes) VALUES
  (1, 1001, TIMESTAMP '2026-06-06 09:00:00', 8.00, 8.10, 7.80, 7.90, 8.00, 0, 86.30, 'On profile; chocolate and almond clear'),
  (2, 1002, TIMESTAMP '2026-06-06 09:20:00', 8.40, 8.60, 8.20, 7.70, 8.10, 0, 87.80, 'Fruit reads well, slightly short development'),
  (3, 1003, TIMESTAMP '2026-06-06 09:40:00', 8.20, 8.30, 8.10, 7.80, 8.00, 0, 87.10, 'Clean cup, good sweetness');

INSERT INTO equipment_telemetry (id, machine_id, recorded_at, vibration_mm_s, exhaust_pa, burner_duty_percent) VALUES
  (1, 'loring-s35', TIMESTAMP '2026-06-05 14:05:00', 1.20, 118.00, 64.00),
  (2, 'loring-s35', TIMESTAMP '2026-06-05 18:05:00', 1.30, 121.00, 62.00),
  (3, 'probats-p25', TIMESTAMP '2026-06-05 16:05:00', 2.80, 96.00, 71.00);
