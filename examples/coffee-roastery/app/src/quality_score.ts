export type RoastBatch = {
  id: number;
  final_temp_c: number;
  target_temp_c: number;
  development_seconds: number;
};

export type CuppingScore = {
  batch_id: number;
  total_score: number;
  defects: number;
};

export function roastVariance(batch: RoastBatch) {
  return {
    batch_id: batch.id,
    temp_delta_c: Number((batch.final_temp_c - batch.target_temp_c).toFixed(2)),
    development_band:
      batch.development_seconds < 90
        ? "short"
        : batch.development_seconds > 115
          ? "long"
          : "on_target",
  };
}

export function releaseRecommendation(batch: RoastBatch, score: CuppingScore) {
  const variance = roastVariance(batch);
  if (score.defects > 0 || Math.abs(variance.temp_delta_c) > 1.2) {
    return "hold_for_review";
  }
  if (score.total_score >= 87 && variance.development_band === "on_target") {
    return "release";
  }
  return "release_with_notes";
}
