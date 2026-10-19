function main(input) {
  const batch = input.batch || {};
  const score = input.score || {};
  const finalTemp = Number(batch.final_temp_c || 0);
  const targetTemp = Number(batch.target_temp_c || 0);
  const tempDelta = Number((finalTemp - targetTemp).toFixed(2));
  const totalScore = Number(score.total_score || 0);
  const defects = Number(score.defects || 0);

  let release = "release_with_notes";
  if (defects > 0 || Math.abs(tempDelta) > 1.2) {
    release = "hold_for_review";
  } else if (totalScore >= 87) {
    release = "release";
  }

  return {
    batch_id: batch.id,
    temp_delta_c: tempDelta,
    total_score: totalScore,
    release,
  };
}
