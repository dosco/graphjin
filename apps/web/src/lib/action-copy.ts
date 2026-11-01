export function actionApprovalAttribution(approverPhrase: string): string {
  return approverPhrase.startsWith("rule ")
    ? `auto-fired by ${approverPhrase}`
    : `approved by ${approverPhrase}`;
}
