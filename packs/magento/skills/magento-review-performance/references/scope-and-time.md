# Magento store scope, currency, and time

Magento websites contain store groups, which contain store views. Resolve
`store_id` from the installed pack's selected store code or from the user's
explicit scope. Admin/all-store scope is not automatically equivalent to every
storefront unless the query deliberately expands it.

Pack financial metrics use base-currency values. Do not sum display-currency
amounts across stores. Name the base currency in the result.

Convert user periods to half-open `[from, to)` boundaries in the configured
Magento timezone before querying. Daylight-saving transitions can produce
local days that are not 24 hours, so derive local calendar boundaries rather
than subtracting a fixed number of seconds.

Keep comparison windows identical in duration, store IDs, currency basis, and
metric definition. If any of those differ, present separate observations rather
than a percentage trend.
