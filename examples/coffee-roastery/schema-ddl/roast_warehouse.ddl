# GraphJin DDL for the read-only BigQuery warehouse simulator.
# Applied locally by the demo runner; live BigQuery DDL migration is unsupported.

type roast_batches {
  id: Bigint! @id
  batch_code: Text!
  profile_id: Bigint!
  green_lot_id: Bigint!
  machine_id: Text!
  started_at: Timestamp!
  ended_at: Timestamp!
  target_temp_c: Numeric
  final_temp_c: Numeric
  development_seconds: Bigint
  drop_reason: Text
  yield_kg: Numeric
}

type roast_sensor_samples {
  id: Bigint! @id
  batch_id: Bigint! @relation(type: roast_batches, field: id)
  recorded_at: Timestamp!
  bean_temp_c: Numeric
  env_temp_c: Numeric
  ror_c_per_min: Numeric
  gas_percent: Numeric
  airflow_percent: Numeric
  drum_rpm: Bigint
}

type qc_cupping_scores {
  id: Bigint! @id
  batch_id: Bigint! @relation(type: roast_batches, field: id)
  scored_at: Timestamp!
  fragrance: Numeric
  flavor: Numeric
  acidity: Numeric
  body: Numeric
  balance: Numeric
  defects: Bigint
  total_score: Numeric
  notes: Text
}

type equipment_telemetry {
  id: Bigint! @id
  machine_id: Text!
  recorded_at: Timestamp!
  vibration_mm_s: Numeric
  exhaust_pa: Numeric
  burner_duty_percent: Numeric
}
