---
name: magento-security-advisories
description: Check the installed Magento or Adobe Commerce patch level against Adobe security bulletins and report critical or otherwise applicable vulnerabilities. Use for scheduled security checks and questions about Magento CVEs.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, adobe-commerce, security, cve, patching]
    category: commerce
    requires_toolsets: [terminal]
    related_skills: [magento-diagnose-platform-health]
---

# Check Magento security advisories

Perform a read-only security check for the installed Magento Open Source or
Adobe Commerce release.

## Source of truth

Fetch the Adobe Magento bulletin index:

`https://helpx.adobe.com/security/security-bulletin.html#magento`

Follow the linked APSB bulletin pages for the affected versions, CVE IDs,
priority, exploitation status, and fixed versions. Use Adobe's security patch
release notes and released-versions page to confirm the current patch level.
Use NVD or CISA KEV only as supporting CVE and exploitation context. Do not
replace Adobe's affected-version or remediation guidance with a third-party
database.

## Procedure

1. Obtain the installed Magento or Adobe Commerce version and patch level from
   the connected pack status or approved Magento health evidence. If it cannot
   be verified, report the result as `unknown`.
2. Fetch the Adobe bulletin index and inspect new or changed Magento bulletins.
   Use Python 3's standard-library HTTPS client from the terminal. This is an
   external read-only request. If the request fails, report that live
   advisory data was unavailable.
3. Compare the installed version with every applicable affected-version range.
   Give priority to bulletins marked critical or exploited in the wild.
4. Report `affected`, `patched`, `unsupported`, or `unknown`. Include the
   bulletin ID, CVE, priority, affected range, fixed release or patch, date,
   and the Adobe remediation URL for each applicable item.
5. Do not apply patches, edit Composer files, rotate keys, or change server
   configuration. Recommend those actions for an administrator to perform and
   verify separately.

Never expose integration tokens, Composer credentials, encryption keys, or
other secrets in the finding.
