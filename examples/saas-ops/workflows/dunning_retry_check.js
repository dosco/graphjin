function main(input) {
  const invoices = input.invoices || [];

  const failed = invoices.filter((invoice) => invoice.status === "failed");
  const retry = failed.filter((invoice) => (invoice.attempts || 0) < 3);
  const escalate = failed.filter((invoice) => (invoice.attempts || 0) >= 3);

  return {
    invoices_checked: invoices.length,
    failed_invoices: failed.length,
    retry_ids: retry.map((invoice) => invoice.id),
    escalate_ids: escalate.map((invoice) => invoice.id),
    recommendation:
      failed.length === 0
        ? "billing healthy: no failed payments"
        : "retry " +
          retry.length +
          " failed payments" +
          (escalate.length > 0
            ? "; escalate " + escalate.length + " to the account manager"
            : ""),
  };
}
