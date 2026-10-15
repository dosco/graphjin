# Contoso Retail Shipping & Return SLA Policy

## Shipping SLAs
- **Standard Shipping:** Orders are scheduled for delivery within 5 business days of the order date.
- **Expedited Shipping:** Orders are scheduled for delivery within 2 business days of the order date.
- **Delivery Confirmation:** An order is deemed complete once `delivery_date` is populated and verified.
- **SLA Breach Threshold:** Any standard order with a transit time exceeding 7 business days is flagged as an SLA breach.

## Return & Refund Guidelines
- **Standard Return Window:** Returns are accepted within 30 days of the recorded `delivery_date`.
- **Condition Requirements:** Returned merchandise must be unopened and in its original manufacturer packaging.
- **Clearance Items:** Products with unit discounts greater than 50% are designated Final Sale and are strictly non-returnable.
- **Refund Method:** Refunds must be credited back to the original payment method and recorded with a valid transaction reference.
