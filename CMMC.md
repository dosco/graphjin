# CMMC 2.0 — Complete Requirements Disposition for OpenNeko

This document gives every CMMC 2.0 requirement an explicit disposition: all
110 NIST SP 800-171 Rev 2 requirements for Level 2 (including the Level 1
requirements derived from FAR 52.204-21), plus all 24 NIST SP 800-172
requirements selected for Level 3. Product evidence is detailed where OpenNeko
contributes a mechanism. People, process, physical, and infrastructure
requirements are retained in the matrix as deployment responsibilities.

> **How to read this.** OpenNeko is a self-hosted software product, not a
> complete information system. CMMC certification applies to the *deploying
> organization's* system boundary; OpenNeko is one component inside it. The
> columns below record which side of that shared-responsibility line each
> control sits on, what OpenNeko itself ships, and the exact features/files
> that back the claim — so an assessor can verify rather than trust.

> **Assessed deployment profile.** This mapping evaluates OpenNeko's company
> deployment configuration, in which an SSO authentication plugin is mandatory
> and the IdP enforces unique user identities, MFA, password policy, account
> lockout, and applicable IdP session controls. OpenNeko's intentional
> unauthenticated solo mode is designed for a single operator on a locally
> controlled system and is outside this CMMC assessment boundary. The SSP and
> configuration baseline must record and enforce this restriction.

**Levels are cumulative:** Level 2 includes all Level 1 controls; Level 3
includes all Level 2 controls plus the 800-172 enhancements.

## Legend

| Column | Values |
|---|---|
| **L1 / L2 / L3** | The level label appears when the requirement applies at that cumulative CMMC level; blank means it is not introduced at that level. |
| **Applicability** | `Product` — OpenNeko must deliver it · `Deployment` — the hosting environment/org delivers it (OS, network, physical) · `Shared` — both contribute · `N/A` — not meaningful for this system |
| **Status** | `✅ Shipped` — OpenNeko's product responsibility is implemented and verifiable in the codebase · `🟡 Partial` — implemented with known limits (stated in rationale) · `❌ Gap` — applicable to the product but not implemented · `Inherited` — must be implemented and evidenced by the deploying organization; this document does not assert that a particular deployment satisfies it · `N/A` |

---

## Access Control (AC)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.1.1 | Limit system access to authorized users, processes, devices | L1 | L2 | L3 | Shared | 🟡 Partial | In the assessed company profile, mandatory SSO provides per-user OIDC sign-in and sessions resolve to `app_user` rows (`apps/web/src/lib/auth.ts`). Device admission and identification remain deployment controls. Intentional unauthenticated solo mode is outside this assessment boundary. |
| 3.1.2 | Limit users to permitted transactions/functions | L1 | L2 | L3 | Product | ✅ Shipped | Role-gated admin routes (`apps/web/src/lib/admin-auth.ts` `requireAdminActor`); agent world-changing actions are proposal-gated by per-org action policies (`db/migrations/0011_action_stack.sql`, `0020_action_policy_created_by.sql`); plugin installs gated by install policy (`db/migrations/0019_install_policy_scope.sql`, `apps/web/src/lib/install-policy-settings.ts`). |
| 3.1.3 | Enforce CUI flow control | | L2 | L3 | Shared | 🟡 Partial | The agent has no terminal and runs no scripts. It reaches data only through the per-run broker, which runs its GraphJin queries with the run's JWT, allows only read-only queries and checks writes against the write policy. An admin can opt into GraphJin's read-only agent instead, which runs in `openneko serve` with the run's JWT. Its web tool reaches only a workflow's declared hosts (`AGENT_RUNTIME.md`); plugin egress is limited to each manifest's allowlist (`PLUGINS.md`). The product does not restrict the outbound internet egress of `openneko serve`, and network-level flow control (firewalling, VLANs, DLP) is the deployment's. |
| 3.1.5 | Least privilege | | L2 | L3 | Product | ✅ Shipped | The agent is treated as untrusted: it has no terminal and no DB access, its file tools reach only the run workspace, it reaches the control plane only through a narrow audited broker, and the model key exists only in its runner and in `openneko serve` for the turn (`AGENT_RUNTIME.md`, `packages/llm/src/work/control-plane.ts`). `openneko serve` runs as an unprivileged user (1001:999). Admin vs member roles gate administration. |
| 3.1.6 | Non-privileged accounts for non-security functions | | L2 | L3 | Product | ✅ Shipped | Mandatory SSO users receive distinct `admin` or non-privileged `member` roles (`apps/web/src/lib/auth.ts` `defaultRoleForGroups`). Members can perform ordinary work and personal-layer operations without an administrative account, while organization settings and security administration are admin-gated (`packages/llm/src/work/authz.ts`, `apps/web/src/lib/admin-auth.ts`). The requirement does not mandate finer-grained RBAC. |
| 3.1.7 | Prevent non-privileged execution of privileged functions; log attempts | | L2 | L3 | Product | ✅ Shipped | Non-admin calls to admin routes return 403 (`apps/web/src/lib/admin-auth.ts`); privileged control-plane calls are recorded with dual identity — human + agent (`db/migrations/0042_dual_identity_audit.sql`, `control_plane_audit`). |
| 3.1.8 | Limit unsuccessful logon attempts | | L2 | L3 | Deployment | Inherited | The assessed company profile delegates interactive authentication to the mandatory IdP; configure and evidence the IdP's lockout/throttling policy. Solo mode is outside scope. |
| 3.1.9 | Privacy/security logon banners | | L2 | L3 | Shared | 🟡 Partial | OpenNeko has no configurable banner on `/signin`; the deployment may provide the required notice at the IdP or access gateway. |
| 3.1.10 | Session lock with pattern-hiding display | | L2 | L3 | Deployment | Inherited | OS-level screen lock on operator workstations; a web app cannot lock the workstation. |
| 3.1.11 | Automatic session termination | | L2 | L3 | Product | ✅ Shipped | Sessions terminate after a defined fixed 12 h TTL (`SESSION_TTL_SECONDS`, `apps/web/src/lib/auth.ts`); rotating `OPENNEKO_SESSION_SECRET` invalidates all sessions globally, and disabling a user invalidates their session on the next request (`getCurrentUser` checks `disabled_at`). An organization choosing a shorter or idle-based condition can enforce it at the mandatory IdP/access gateway. |
| 3.1.12 | Monitor and control remote access sessions | | L2 | L3 | Shared | 🟡 Partial | All web/API access is session-authenticated and privileged calls are audit-chained (see AU rows). VPN/gateway placement and monitoring of the path to the deployment is the operator's. |
| 3.1.13 | Cryptographic protection of remote access | | L2 | L3 | Deployment | Inherited | The web app binds locally (`localhost:3000`); remote exposure requires the operator's TLS-terminating reverse proxy/VPN. Session cookies are marked `secure` in production (`apps/web/src/lib/auth.ts` `writeSessionCookie`). |
| 3.1.14 | Route remote access via managed access points | | L2 | L3 | Deployment | Inherited | Deployment network architecture. |
| 3.1.15 | Authorize remote execution of privileged commands | | L2 | L3 | Product | ✅ Shipped | Every world-changing agent action becomes a proposal requiring human approval unless an explicit auto-fire rule exists (`FEATURES.md` "Approval gates"; `action_request` tables, `db/migrations/0011_action_stack.sql`). |
| 3.1.16 | Authorize wireless access before connection | | L2 | L3 | Deployment | Inherited | No wireless surface in the product; authorization is enforced by deployment network controls. |
| 3.1.17 | Protect wireless access with authentication and encryption | | L2 | L3 | Deployment | Inherited | Deployment wireless identity and encryption controls. |
| 3.1.18 | Control connection of mobile devices | | L2 | L3 | Deployment | Inherited | OpenNeko ships no mobile agent; mobile browser access is governed by deployment MDM and access controls. |
| 3.1.19 | Encrypt CUI on mobile devices and platforms | | L2 | L3 | Deployment | Inherited | Endpoint/MDM encryption control. |
| 3.1.20 | Verify/control connections to external systems | L1 | L2 | L3 | Product | 🟡 Partial | Connections from the agent to the stack are enumerable: the broker, over loopback from its runner, which also carries its GraphJin queries, and GraphJin's read-only agent in `openneko serve` with the run's JWT when an admin opts in. The agent has no terminal. Its web tool reaches only declared hosts and plugin egress follows each manifest allowlist (`AGENT_RUNTIME.md`, `PLUGINS.md`). `openneko serve` uses the stack's default network to reach model APIs and data sources, and the product does not restrict its outbound internet access. |
| 3.1.21 | Limit portable storage on external systems | | L2 | L3 | Deployment | Inherited | Host/endpoint control. |

## Audit & Accountability (AU)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.3.1 | Create and retain audit logs | | L2 | L3 | Shared | 🟡 Partial | Important run, proposal, decision, execution, and broker events are recorded, including a per-org hash chain (`db/migrations/0046_audit_chain.sql`, `packages/llm/src/workflows/audit-chain.ts`). Chain writes are best-effort, and OpenNeko does not enforce an organizational retention period; retention and durable export are deployment responsibilities. |
| 3.3.2 | Trace actions to individual users | | L2 | L3 | Product | ✅ Shipped | In the assessed SSO profile, dual-identity audit records both the individual human and the agent acting for them (`db/migrations/0042_dual_identity_audit.sql`, `0031_actor_in_runs.sql`; `FEATURES.md` "no anonymous actions"). Solo mode is outside scope. |
| 3.3.4 | Alert on audit logging failure | | L2 | L3 | Product | ✅ Shipped | A failed append emits a structured critical `security.audit_logging_failure` event to stderr, marks process-local audit health degraded for `GET /health/security`, and can deliver a non-blocking alert through an independently configured HTTPS webhook (`packages/llm/src/workflows/audit-chain.ts`, `apps/worker/src/admin-server.ts`, `INSTALL.md` "Audit logging failure alerts"). |
| 3.3.5 | Correlate audit records for investigation | | L2 | L3 | Product | ✅ Shipped | Audit trail is exportable for auditors and SIEM tools (`FEATURES.md` "Tamper-evident log"); natural-language audit queries ("what did the assistant do yesterday and who approved it?") give a readable timeline. |
| 3.3.6 | Audit reduction and report generation | | L2 | L3 | Product | ✅ Shipped | Plain-language audit timeline plus structured export (`FEATURES.md` "Audit in plain language"). |
| 3.3.7 | Clocks synced to authoritative time source | | L2 | L3 | Deployment | Inherited | Container/host NTP. Audit rows use DB timestamps (single Postgres = single clock). |
| 3.3.8 | Protect audit info from unauthorized access/modification | | L2 | L3 | Shared | 🟡 Partial | The hash chain detects edits or deletions that leave a later link, and appends serialize per org (`packages/llm/src/workflows/audit-chain.ts`). It cannot detect deletion of the tail or truncation of a suffix, and the database table itself is not immutable; database roles, durable export, and external anchoring are deployment controls. |
| 3.3.9 | Limit audit management to privileged users | | L2 | L3 | Shared | 🟡 Partial | Product audit surfaces are admin-gated and no delete/edit API exists (`apps/web/src/lib/admin-auth.ts`). Direct database access and separation of audit administration from general database administration must be enforced by deployment roles. |

## Configuration Management (CM)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.4.1 | Baseline configurations and inventories | | L2 | L3 | Shared | 🟡 Partial | The stack is declared in versioned compose files (`compose.yml`, `apps/openneko/assets/compose/core.yml`), but some image references use floating tags such as `latest` rather than immutable digests. Installed solution packs are inventoried in the database with their manifest hash (`PACKS.md`). Host OS baseline and the approved image/version baseline are the deployment's. |
| 3.4.2 | Enforce security configuration settings | | L2 | L3 | Product | ✅ Shipped | Secure-by-default posture: in every stack mode the agent has no terminal, OpenNeko runs no scripts, and agent turns run in `openneko serve` as an unprivileged user (`AGENT_RUNTIME.md`); install policy defaults deny git-URL skill installs (`/admin/settings/security`); current secret-store writers create files with mode `0600` (`packages/secrets/src/secrets-store.ts`, `packages/secret-crypt/src/index.ts`). |
| 3.4.5 | Access restrictions for configuration changes | | L2 | L3 | Product | ✅ Shipped | Settings and install policy changes require the admin role (`apps/web/src/lib/admin-auth.ts`); agent-proposed configuration changes go through approval gates (`FEATURES.md`). |
| 3.4.6 | Least functionality | | L2 | L3 | Product | ✅ Shipped | The agent has only declared tools: file tools confined to the run workspace, upload tools, broker MCP tools (GraphJin's tools among them), GraphJin's read-only agent when an admin opts in, and a web tool limited to declared hosts. It has no terminal, and OpenNeko runs no skill or workflow scripts (`AGENT_RUNTIME.md`). Plugins run in sandboxes with only declared capabilities; on hosts that can't run the sandbox runtime, the plugin subsystem is disabled rather than degraded to unsandboxed (`PLUGINS.md`). |
| 3.4.7 | Restrict nonessential programs/ports/protocols/services | | L2 | L3 | Shared | 🟡 Partial | The packaged stack publishes only the web port; the agent runtime talks over a Unix socket (`/run/openneko` holds only `serve.sock`), and the agent image ships no shell tool and no document toolchain; worker admin endpoint is loopback-only, never exposed (`apps/web/src/lib/auth.ts` topology note). Outbound egress from `openneko serve` and host firewalling are the deployment's. |
| 3.4.8 | Allow-by-exception (application allowlisting) | | L2 | L3 | Shared | 🟡 Partial | Applied to the product's extension points: solution packs are validated and hashed at install, and git-URL skill installs are off unless an admin enables them in the install policy. OS-level application allowlisting is the deployment's. |
| 3.4.9 | Control and monitor user-installed software | | L2 | L3 | Product | ✅ Shipped | Install policy enforced at CLI, worker, and web (`FEATURES.md` "Install policy"); policy changes flag (not yank) pre-existing installs for manual review (`PLUGINS.md`); unverified installs print loud warnings and remain sandboxed. |

## Identification & Authentication (IA)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.5.1 | Identify users, processes, devices | L1 | L2 | L3 | Shared | 🟡 Partial | Mandatory SSO creates a unique `app_user` per person keyed by the IdP-stable `sub` (`apps/web/src/lib/auth.ts` `upsertUserFromIdentity`), while runs record the acting human and agent backend. Device identity is a deployment responsibility. Solo mode is outside scope. |
| 3.5.2 | Authenticate identities before access | L1 | L2 | L3 | Product | ✅ Shipped | In the assessed company profile, mandatory SSO gates app access with a valid session (HMAC-signed cookie, constant-time verification, expiry enforced — `apps/web/src/lib/auth.ts` `decodeSession`); the OIDC flow guards against CSRF/open redirect via signed state cookies (`writeStateCookie`, `encodeReturnPath`). |
| 3.5.3 | Multifactor authentication | | L2 | L3 | Shared | 🟡 Partial | Delegated to the mandatory IdP via the OIDC auth plugin (e.g. Scalekit); the operator must configure and evidence MFA enforcement there. |
| 3.5.4 | Replay-resistant authentication | | L2 | L3 | Shared | 🟡 Partial | The mandatory IdP supplies the OIDC authorization-code flow. OpenNeko uses a random signed state cookie, deletes it from the browser on callback, and issues HMAC-bound expiring `httpOnly`/`secure`/`sameSite=lax` session cookies (`apps/web/src/lib/auth.ts`). State consumption is not tracked server-side, so IdP configuration and protocol guarantees remain part of the control. |
| 3.5.5 | Prevent identifier reuse | | L2 | L3 | Shared | 🟡 Partial | OpenNeko generates high-entropy user IDs (`usr_` + 72-bit random, `apps/web/src/lib/auth.ts`) and refuses to bind an email already attached to a different SSO `sub`. The IdP owns identifier retirement/reuse policy; OpenNeko has no explicit tombstone mechanism for retired identifiers. |
| 3.5.6 | Disable identifiers after inactivity | | L2 | L3 | Shared | 🟡 Partial | In the mandatory-SSO profile, the IdP must define and enforce identifier inactivity/disable policy. OpenNeko supports immediate application-level disable through `disabled_at`, and disabled users' existing sessions fail on the next user lookup (`db/migrations/0035_app_user_disabled.sql`; `getCurrentUser`). OpenNeko does not automatically mirror an IdP inactivity decision without a provisioning/synchronization integration. |
| 3.5.7 | Enforce password complexity and changed characters | | L2 | L3 | Deployment | Inherited | Interactive authentication is delegated to the mandatory IdP; configure and evidence its password-complexity policy. The setup wizard password is a database credential, not an application login. |
| 3.5.8 | Prohibit password reuse for defined generations | | L2 | L3 | Deployment | Inherited | Configure and evidence the mandatory IdP's password-history policy. |
| 3.5.9 | Restrict temporary passwords and require immediate change | | L2 | L3 | Deployment | Inherited | Configure and evidence the mandatory IdP's temporary-credential policy. |
| 3.5.10 | Cryptographically protected password/credential storage | | L2 | L3 | Product | ✅ Shipped | All stored credentials (tokens, keys, passwords) are encrypted at rest with AES-256-GCM `enc:v1` (`packages/secret-crypt/src/index.ts`, `apps/openneko/internal/config/crypt.go`, `SECRETS.md`); credentials are never typed into chat and never enter model context (`FEATURES.md` "Credentials stay out of chat"). |
| 3.5.11 | Obscure authentication feedback | | L2 | L3 | Product | ✅ Shipped | Plugin secret prompts use hidden input at the CLI (`PLUGINS.md`); browser password fields mask input. |

## Media Protection (MP)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.8.3 | Sanitize media before disposal/reuse | L1 | L2 | L3 | Deployment | Inherited | Physical media handling is the operator's. Product-side note: all state lives in one Postgres + one secrets file, so the surface to sanitize is small and known (`README.md` "single Postgres"). |
| 3.8.6 | Encrypt CUI on media during transport | | L2 | L3 | Deployment | Inherited | Backup transport is the operator's; encrypting backups at rest is theirs too (see 3.8.9). |
| 3.8.7 | Control removable media | | L2 | L3 | Deployment | Inherited | Host/endpoint control. |
| 3.8.9 | Protect confidentiality of backup CUI | | L2 | L3 | Shared | 🟡 Partial | Secrets inside any backup are already ciphertext (`enc:v1`, `packages/secret-crypt`); business data in Postgres dumps is plaintext — the operator must encrypt backups. |

## Maintenance (MA) / Risk Assessment (RA)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.7.5 | MFA for nonlocal maintenance | | L2 | L3 | Deployment | Inherited | SSH/remote administration of the host is outside the product. |
| 3.11.2 | Vulnerability scanning | | L2 | L3 | Deployment | Inherited | The assessed organization must periodically and event-driven scan the deployed application, containers, hosts, dependencies, and surrounding boundary with its approved vulnerability-management tooling. OpenNeko's repository CI does not substitute for—or constrain—the client's authenticated deployment scanning. |
| 3.11.3 | Remediate vulnerabilities | | L2 | L3 | Shared | 🟡 Partial | Release and smoke-gated deployment workflows provide a fast path to publish and deploy fixes (`.github/workflows/release-please.yml`, `post-release-smoke.yml`, `deploy.yml`), but without 3.11.2 scanning and remediation tracking there is no systematic detection-to-closure process. |

## Remaining Level 2 requirements — complete disposition

The rows below are the requirements previously omitted from the product-focused
sections. Together with the detailed rows above, they complete the 110-requirement
Level 2 set. `Inherited` here means OpenNeko supplies no standalone mechanism;
the deploying organization must implement and evidence the requirement across
the assessed system boundary.

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.1.4 | Separate duties to reduce malicious activity without collusion | | L2 | L3 | Shared | 🟡 Partial | OpenNeko separates admin/member and human/agent roles, but it does not implement an organizational separation-of-duties matrix or require two distinct people for sensitive operations. |
| 3.1.22 | Control CUI on publicly accessible systems | L1 | L2 | L3 | Shared | 🟡 Partial | OpenNeko is designed for controlled access and supplies no public-publishing workflow or CUI review gate. The organization must prohibit public exposure or provide review/removal procedures and perimeter controls. |
| 3.2.1 | Security awareness for managers, administrators, and users | | L2 | L3 | Deployment | Inherited | Organizational training program; OpenNeko provides no LMS or training-attestation mechanism. |
| 3.2.2 | Train personnel for assigned security duties | | L2 | L3 | Deployment | Inherited | Organizational role-based training and records. |
| 3.2.3 | Insider-threat awareness training | | L2 | L3 | Deployment | Inherited | Organizational training and reporting program. |
| 3.3.3 | Review and update logged events | | L2 | L3 | Shared | 🟡 Partial | OpenNeko records fixed product event classes, but the organization must define, periodically review, and update which system-boundary events are logged. No product workflow manages that review. |
| 3.4.3 | Track, review, approve/disapprove, and log system changes | | L2 | L3 | Shared | 🟡 Partial | Versioned compose/config artifacts and product audit records contribute evidence, but infrastructure changes and the organizational approval process are deployment responsibilities. |
| 3.4.4 | Analyze security impact before changes | | L2 | L3 | Deployment | Inherited | Requires an organizational pre-change security-impact analysis and evidence; smoke tests do not replace that analysis. |
| 3.6.1 | Establish operational incident-handling capability | | L2 | L3 | Deployment | Inherited | OpenNeko exports audit data and behavior alerts, but incident preparation, detection/analysis, containment, recovery, and user response are organizational capabilities. |
| 3.6.2 | Track, document, and report incidents | | L2 | L3 | Deployment | Inherited | Organizational incident case management and reporting to required authorities; OpenNeko has no incident-reporting workflow. |
| 3.6.3 | Test incident-response capability | | L2 | L3 | Deployment | Inherited | Organizational exercises and retained test evidence. |
| 3.7.1 | Perform system maintenance | | L2 | L3 | Deployment | Inherited | Host, platform, database, network, and application maintenance program. |
| 3.7.2 | Control maintenance tools, techniques, mechanisms, and personnel | | L2 | L3 | Deployment | Inherited | Deployment maintenance authorization and tooling controls. |
| 3.7.3 | Sanitize equipment removed for off-site maintenance | | L2 | L3 | Deployment | Inherited | Physical equipment and media handling. |
| 3.7.4 | Check diagnostic/test media for malicious code | | L2 | L3 | Deployment | Inherited | Endpoint/media protection in the hosting environment. |
| 3.7.6 | Supervise maintenance personnel lacking required access | | L2 | L3 | Deployment | Inherited | Organizational personnel and maintenance-session supervision. |
| 3.8.1 | Physically control and securely store media containing CUI | | L2 | L3 | Deployment | Inherited | Physical and backup-media protection. |
| 3.8.2 | Limit access to CUI on system media | | L2 | L3 | Deployment | Inherited | Storage, backup, host, and physical access controls. |
| 3.8.4 | Mark media with CUI markings and distribution limits | | L2 | L3 | Deployment | Inherited | Organizational media-marking procedure; OpenNeko does not label exported artifacts as CUI. |
| 3.8.5 | Control and account for media during transport | | L2 | L3 | Deployment | Inherited | Organizational custody and transport controls. |
| 3.8.8 | Prohibit portable storage without an identifiable owner | | L2 | L3 | Deployment | Inherited | Endpoint/removable-media policy and enforcement. |
| 3.9.1 | Screen individuals before granting CUI-system access | | L2 | L3 | Deployment | Inherited | Personnel screening and authorization process. |
| 3.9.2 | Protect systems during and after personnel actions | | L2 | L3 | Shared | 🟡 Partial | Disabling an `app_user` invalidates application access on the next request, but transfer/termination triggers, IdP deprovisioning, asset return, and broader access removal are organizational responsibilities. |
| 3.10.1 | Limit physical access to systems and operating environments | L1 | L2 | L3 | Deployment | Inherited | Facility, data-center, workstation, and hardware controls. |
| 3.10.2 | Protect and monitor facilities and support infrastructure | | L2 | L3 | Deployment | Inherited | Facility and hosting-provider controls. |
| 3.10.3 | Escort visitors and monitor visitor activity | L1 | L2 | L3 | Deployment | Inherited | Physical security process. |
| 3.10.4 | Maintain physical-access audit logs | L1 | L2 | L3 | Deployment | Inherited | Facility access-control system and records. |
| 3.10.5 | Control and manage physical-access devices | L1 | L2 | L3 | Deployment | Inherited | Badge, key, lock, and credential management. |
| 3.10.6 | Safeguard CUI at alternate work sites | | L2 | L3 | Deployment | Inherited | Remote-work, endpoint, facility, and network policy. |
| 3.11.1 | Periodically assess organizational risk | | L2 | L3 | Deployment | Inherited | Organizational risk assessment covering the complete system boundary. This code mapping is an input, not the assessment itself. |
| 3.12.1 | Periodically assess security controls | | L2 | L3 | Deployment | Inherited | Formal assessment using NIST SP 800-171A/CMMC objectives and retained evidence. |
| 3.12.2 | Maintain plans of action for deficiencies | | L2 | L3 | Deployment | Inherited | Organizational operational plan-of-action and CMMC POA&M governance. |
| 3.12.3 | Continuously monitor security-control effectiveness | | L2 | L3 | Shared | 🟡 Partial | Product behavior monitoring and audit export contribute signals; the organization must monitor the full boundary and control set. |
| 3.12.4 | Develop and update the system security plan | | L2 | L3 | Deployment | Inherited | The organization must produce the authoritative SSP describing scope, environment, connections, and implementation. This file is supporting product evidence only. |
| 3.13.2 | Use secure architecture, engineering, and development principles | | L2 | L3 | Shared | 🟡 Partial | Separating the control plane (web and worker) from the per-run agent runner and `openneko serve`, with no terminal or script execution for the agent, demonstrates product security architecture (`AGENT_RUNTIME.md`), while the organization must address the complete system architecture and SDLC. |
| 3.14.3 | Monitor security alerts/advisories and respond | | L2 | L3 | Shared | 🟡 Partial | OpenNeko provides product behavior alerts but has no vendor/dependency advisory ingestion and response workflow; the organization must monitor relevant sources and track action. |

## System & Communications Protection (SC)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.13.1 | Boundary protection | L1 | L2 | L3 | Product | ✅ Shipped | Untrusted components sit outside the control plane: the agent runs in a short-lived runner process and in `openneko serve`, with no terminal and no DB access, and each plugin runs in its own sandbox; the only trusted control plane is the web and worker processes (`AGENT_RUNTIME.md`). External perimeter (firewall/proxy) is the deployment's. |
| 3.13.3 | Separate user functionality from system management | | L2 | L3 | Product | ✅ Shipped | Control plane (DB, secrets, policy, approvals) is separated from the agent runtime: `runChatTurn` splits into host-side prologue/epilogue and the agent loop, which runs in a separate runner process and `openneko serve` (`AGENT_RUNTIME.md`, `packages/llm/src/work/`). |
| 3.13.4 | Prevent unauthorized info transfer via shared resources | | L2 | L3 | Product | ✅ Shipped | Per-plugin isolated sandboxes; per-operator OAuth tokens are stored per-person and injected per invocation, never shared (`PLUGINS.md` "Per-person integrations", `FEATURES.md`). The agent has no terminal and runs no scripts. Its file tools reach only its own run's workspace, and its upload tools only the thread's uploads. Run folders under `/var/lib/openneko/agent-runs` (owner 1001:1001, mode 2770) are shared only by web, worker and serve (`AGENT_RUNTIME.md` "The shared runs folder"). |
| 3.13.5 | Subnetworks for publicly accessible components | L1 | L2 | L3 | Deployment | Inherited | OpenNeko is not designed to be public-facing; exposure topology (DMZ, reverse proxy) is the operator's. |
| 3.13.6 | Deny-all, permit-by-exception network traffic | | L2 | L3 | Product | 🟡 Partial | The agent reaches the control plane only through the broker, its web tool only declared hosts, and plugins only their manifest hosts (`AGENT_RUNTIME.md`, `PLUGINS.md`). `openneko serve` uses the stack's default network to reach model APIs and data sources, and the product permits its outbound internet egress by default; denying it is the deployment's (firewall that network). |
| 3.13.7 | Prevent split tunneling | | L2 | L3 | Deployment | Inherited | Endpoint/VPN configuration. |
| 3.13.8 | Encrypt CUI in transit | | L2 | L3 | Shared | 🟡 Partial | Model/plugin egress is HTTPS; the agent runtime's internal hops use local Unix sockets, not the network (`AGENT_RUNTIME.md`). The web UI itself serves HTTP on localhost — remote access requires the operator's TLS reverse proxy. |
| 3.13.9 | Terminate network connections at the end of communications sessions | | L2 | L3 | Deployment | Inherited | Connection lifetime and termination are controlled by the client's TLS reverse proxy, load balancer, VPN, firewall, and container/network stack. OpenNeko's authentication-cookie lifetime is addressed separately under 3.1.11 and is not the network-connection requirement in 3.13.9. |
| 3.13.10 | Cryptographic key management | | L2 | L3 | Shared | 🟡 Partial | A local 32-byte key is generated at `~/.config/openneko/secret-key` and newly created keys are set to mode 0600 (`packages/secret-crypt/src/index.ts`); enterprise deployments can use external Infisical-backed secrets (`SECRETS.md`). Existing key permissions are not validated or repaired, and local `enc:v1` data has no key identifier or built-in re-encryption workflow, so lifecycle, backup, rotation, and separation controls remain with the deployment. |
| 3.13.11 | FIPS-validated cryptography | | L2 | L3 | Product | ❌ Gap | Algorithms are FIPS-approved (AES-256-GCM, SHA-256, HMAC-SHA-256) but the crypto modules (Node `node:crypto`/OpenSSL as shipped, Go stdlib) are not FIPS-140-validated builds. Deployments requiring FIPS validation need FIPS-mode OpenSSL/BoringCrypto builds. |
| 3.13.12 | Control collaborative computing devices | | L2 | L3 | N/A | N/A | No camera/mic surface. |
| 3.13.13 | Control and monitor mobile code | | L2 | L3 | Product | ✅ Shipped | The mobile-code problem *is* the product's core threat model: OpenNeko runs no code the LLM generates. The agent has no terminal, skills and workflows run no scripts, and a saved workflow with a script step fails with a clear error. Third-party plugin code runs in its own sandbox, and there is no unsandboxed plugin mode (`AGENT_RUNTIME.md`, `PLUGINS.md`). |
| 3.13.14 | Control VoIP | | L2 | L3 | N/A | N/A | No VoIP surface. |
| 3.13.15 | Protect authenticity of communication sessions | | L2 | L3 | Product | ✅ Shipped | HMAC-SHA-256-signed session cookies with constant-time comparison (`apps/web/src/lib/auth.ts`); per-run broker tokens bound to run, org and thread; agent runtime sockets restricted by file ownership (`AGENT_RUNTIME.md`); signed single-use OIDC state. |
| 3.13.16 | Protect CUI at rest | | L2 | L3 | Deployment | Inherited | OpenNeko application-encrypts credential material with AES-256-GCM (`packages/secret-crypt`, `SECRETS.md`, `db/migrations/0047_data_source_secret.sql`) but intentionally does not impose application-layer encryption on general PostgreSQL rows. The client must select, operate, and evidence encryption for PostgreSQL storage and backups—such as encrypted disks/volumes, managed-database encryption, or TDE—within its assessed deployment boundary. |

## System & Information Integrity (SI)

| Control | Technical requirement | L1 | L2 | L3 | Applicability | Status | Rationale / evidence |
|---|---|:-:|:-:|:-:|---|---|---|
| 3.14.1 | Identify and correct system flaws | L1 | L2 | L3 | Shared | 🟡 Partial | Release and post-release smoke workflows provide a path to distribute and gate corrections (`.github/workflows/release-please.yml`, `post-release-smoke.yml`, `deploy.yml`). The repository has no systematic dependency/container vulnerability scanning, so identification and remediation tracking require additional product and deployment processes. |
| 3.14.2 | Protect against malicious code at designated locations | L1 | L2 | L3 | Deployment | Inherited | Host/container EDR or anti-malware is the operator's. OpenNeko runs no agent-generated code and sandboxes plugin code; this limits untrusted-code impact but does not replace malicious-code protection. |
| 3.14.4 | Update malicious-code protection when releases are available | L1 | L2 | L3 | Deployment | Inherited | Deployment EDR/anti-malware update configuration and evidence. |
| 3.14.5 | Perform periodic and real-time malicious-code scans | L1 | L2 | L3 | Deployment | Inherited | Deployment scanning configuration and evidence. |
| 3.14.6 | Monitor systems and traffic for attacks | | L2 | L3 | Product | ✅ Shipped | Behavioral threshold monitoring on the agent: control-plane call rates per run, action requests/hour, memory writes/hour raise `behavior_alert` rows and dispatch `security.behavior_threshold` events for paging (`packages/llm/src/work/behavior-monitor.ts`, `db/migrations/0043_behavior_alerts.sql`). Network IDS is the deployment's. |
| 3.14.7 | Identify unauthorized use | | L2 | L3 | Product | ✅ Shipped | Dual-identity audit means no anonymous privileged action (`0042_dual_identity_audit.sql`); behavior alerts flag activity outside the envelope; security profiles (solo/team/org/hardened) scale alarm sensitivity (`FEATURES.md` "Security profiles"). |

## Level 3 only — all 24 selected NIST SP 800-172 requirements

| Control | Technical requirement | Applicability | Status | Rationale / evidence |
|---|---|---|---|---|
| AC 3.1.2e | Restrict CUI access to organization-controlled endpoints | Deployment | Inherited | Endpoint/NAC control. |
| AC 3.1.3e | Secure transfer between security domains | Shared | 🟡 Partial | The serve boundary is a controlled transfer point: model keys reach `openneko serve` only with a turn, and serve holds each key only for that turn. The agent has no terminal or script runtime that could read them (`AGENT_RUNTIME.md`). Broader cross-domain solutions are the deployment's. |
| AT 3.2.1e | Provide advanced-threat awareness and update it for changing threats | Deployment | Inherited | Organizational threat-awareness program and training records. |
| AT 3.2.2e | Conduct role-tailored practical threat-training exercises | Deployment | Inherited | Organizational exercises, feedback, and retained participation evidence. |
| CM 3.4.1e | Maintain an authoritative repository of approved and integrity-verified components | Shared | 🟡 Partial | Versioned configuration and inventories contribute, and Linux release downloads are checksum-verified. Plugin integrity metadata is not independently verified after `npm install`, some images float, and the organization must maintain the boundary-wide authoritative repository. |
| CM 3.4.2e | Automated detection/response to misconfigured or unauthorized components | Product | 🟡 Partial | `openneko doctor`/`status` probe what's actually serving (`FEATURES.md` "Honest status"); policy changes automatically flag non-conforming installed plugins for removal (`PLUGINS.md`). No continuous drift detection for host config. |
| CM 3.4.3e | Automated asset inventory | Shared | 🟡 Partial | Installed solution packs are recorded in the database (`PACKS.md`); container inventory is declared in compose. Full-environment asset discovery is the deployment's. |
| IA 3.5.1e | Bidirectional cryptographic authentication between components | Product | 🟡 Partial | Agent runs authenticate to the broker with per-run bearer tokens; the agent runtime's sockets rely on operating-system file ownership rather than cryptographic authentication (`AGENT_RUNTIME.md`). Not universal across every internal hop (worker admin endpoint relies on loopback isolation). |
| IA 3.5.3e | Block untrusted assets from connecting | Deployment | Inherited | NAC is network infrastructure. |
| IR 3.6.1e | Maintain a 24/7 security operations center capability | Deployment | Inherited | Organizational or contracted SOC capability; OpenNeko audit exports and alerts are possible inputs. |
| IR 3.6.2e | Maintain a cyber incident response team deployable within 24 hours | Deployment | Inherited | Organizational staffing, procedures, communications, and exercise evidence. |
| PS 3.9.2e | Review adverse personnel information and take access action | Deployment | Inherited | Personnel-security and access-governance process; OpenNeko user disable can execute an application-level removal decision. |
| RA 3.11.1e | Perform threat-informed risk assessment | Deployment | Inherited | Organization-wide risk assessment informed by current threat intelligence. |
| RA 3.11.2e | Threat hunting with threat intelligence | Deployment | Inherited | Organizational capability; the exportable audit chain and behavior alerts are the product's food for it. |
| RA 3.11.3e | Use advanced automation and analytics to identify risk | Deployment | Inherited | Boundary-wide security analytics capability; OpenNeko's fixed behavior thresholds are not a complete advanced risk-identification solution. |
| RA 3.11.4e | Document the rationale for selected security solutions | Deployment | Inherited | Organizational risk analysis and SSP documentation. |
| RA 3.11.5e | Assess security-solution effectiveness at least annually and on relevant triggers | Deployment | Inherited | Organizational testing and assessment driven by threat information and incidents. |
| RA 3.11.6e | Assess, respond to, and monitor supply-chain risk | Deployment | Inherited | Organizational supplier and component risk-management program. Plugin source/integrity observations in this mapping are inputs only. |
| RA 3.11.7e | Develop and maintain a supply-chain risk-management plan | Deployment | Inherited | Organizational plan, roles, processes, and evidence. |
| CA 3.12.1e | Conduct independent penetration testing at least annually | Deployment | Inherited | Organizationally commissioned independent testing of the complete Level 3 scope. Repository tests and release smoke tests are not penetration tests. |
| SC 3.13.4e | Physical/logical isolation techniques | Product | ✅ Shipped | Logical isolation is the architecture: the control plane (web and worker), the per-run agent runner and `openneko serve` are separate processes, and each plugin has its own sandbox. The agent has no terminal, no DB access and no script runtime, and its file tools stay inside the run workspace (`AGENT_RUNTIME.md`). |
| SI 3.14.1e | Verify critical software integrity with a root of trust | Shared | 🟡 Partial | Linux release checksums contribute, but plugin artifacts are not independently verified after installation and there is no hardware-backed root of trust or signed-boot chain. Platform integrity remains a deployment responsibility. |
| SI 3.14.3e | Secure or isolate specialized assets | Deployment | Inherited | The organization must identify specialized assets and either assess them against all requirements or isolate them in purpose-specific networks with SSP-documented mitigations. |
| SI 3.14.6e | Use threat indicators to guide intrusion detection and hunting | Deployment | Inherited | SOC/IDS capability using open, commercial, and DoD-provided threat sources. OpenNeko can export audit data but does not ingest IOC feeds into an IDS. |

---

## Summary of open product responsibilities

This list is intentionally limited to rows whose applicability is `Product`
and whose status is not `✅ Shipped`. Shared and inherited requirements remain
visible in the complete matrix but are not mislabeled as OpenNeko product gaps.

1. **3.13.11 — Gap:** OpenNeko's shipped Node/OpenSSL and Go cryptographic modules are not FIPS 140-validated builds. This matters wherever those modules provide CUI confidentiality rather than a client-controlled FIPS boundary doing so.
2. **CM 3.4.2e — Partial:** product status and policy reconciliation exist, but continuous automated drift detection/remediation does not cover every OpenNeko component.
3. **IA 3.5.1e — Partial:** agent runs authenticate to the broker with per-run tokens and the agent runtime's sockets rely on file ownership; bidirectional cryptographic authentication is not universal across every internal component hop.
4. **3.1.20 — Partial:** the agent has no terminal and reaches the stack only through the broker and, when an admin opts in, GraphJin's read-only agent, but the product does not restrict the outbound internet access of `openneko serve`.
5. **3.13.6 — Partial:** `openneko serve` runs on the stack's default network, and the product permits its outbound internet egress by default; deny-by-default egress is left to a firewall on that network.

*Last reviewed: 2026-07-13. CMMC references: 32 CFR Part 170; NIST SP 800-171 Rev 2; NIST SP 800-172.*
