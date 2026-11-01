---
name: magento-run-promotions
description: Design, preview, and govern Magento sales rules and coupon batches with discount, duration, count, and projected-exposure caps. Use when a user asks to create, edit, end, delete, or generate coupons for a promotion.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, promotions, sales-rules, coupons, discounts, exposure]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-manage-catalog, magento-review-performance]
---

# Run Magento promotions

Resolve the website/store scope, base currency, customer groups, active dates,
discount type/value, coupon count, usage limits, conditions, and expected order
volume. Calculate and state projected exposure before proposing the action.

Use only `magento.manage_promotions`. A human administrator must approve every
promotion operation. Reject a free-cart outcome, discount above the
stored ceiling, duration below the minimum, coupon count above its cap, or
projected exposure above its cap. Do not weaken the rule to make it pass.

For an edit or delete, read the current sales rule and show the exact before
image. After execution, read the rule back and report only `applied` when the
requested fields reconcile. Coupon generation is not safely reversible; make
that explicit before approval and never retry an ambiguous result.

Boundary: Never create a free-cart or uncapped promotion, never bypass `magento.manage_promotions` with SQL, raw GraphQL, or raw REST, and never execute a promotion automatically.
