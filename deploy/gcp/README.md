# OpenNeko on a GCP Compute Engine VM

Terraform for the single-VM shape discussed: a real Docker daemon (OpenNeko's
Compose stack requires one -- it does not run on bare containerd, which is all
GKE nodes provide), fronted by Caddy on 443 with
automatic Let's Encrypt TLS for your own domain.

## What this creates

- One Compute Engine VM (Ubuntu 22.04, Docker Engine + Compose plugin, Caddy,
  the `openneko` CLI pre-installed via startup script).
- A static external IP, so the domain's A record survives VM rebuilds.
- Firewall rules opening 80 (ACME challenge + HTTP->HTTPS redirect) and 443
  (the app, via Caddy) to the internet -- end users hit these directly, no
  IAP in their path. OpenNeko's web port (3000) is never opened at the
  firewall -- it stays loopback-only per OpenNeko's own defaults, reachable
  only through Caddy.
- SSH (22) has **no public route at all**. It's reachable only via an IAP
  tunnel from Google's fixed `35.235.240.0/20` relay range, gated by IAM
  (`admin_members`), with OS Login handling authentication -- no SSH keys to
  generate, distribute, or rotate.
- Optionally, a Cloud DNS `A` record (`manage_dns = true`) if you host the
  zone there; otherwise you point your existing DNS provider at the output IP.

## What this deliberately does NOT do

`openneko setup` (admin password, data source, model provider + API key)
is left as a manual step you SSH in and run yourself, rather than templated
into Terraform variables/state. Feeding secrets through Terraform metadata
means they sit in plaintext in both the instance's metadata and your
Terraform state file -- avoidable for a one-time interactive wizard.  If you
want a fully unattended build, look at `openneko setup --admin-password
--provider --provider-key ...` (documented in INSTALL.md) and wire those
through Secret Manager + the VM's service account instead of raw metadata.

## Usage

```sh
cp terraform.tfvars.example terraform.tfvars   # gitignored -- your real values go here, never in the .example
# edit terraform.tfvars: project_id, domain, tls_email, admin_members

terraform init
terraform plan
terraform apply
```

`admin_members` defaults to `[]` in the checked-in config -- this is a shared,
public repo, so no identity is granted access out of the box. You set your
own admins locally in `terraform.tfvars`, which `.gitignore` keeps out of
version control alongside `*.tfstate` (state can hold plaintext resource
details like service account emails).

Then follow the `next_steps` output -- DNS, waiting for the startup script,
running `openneko setup`, and (if applicable) setting the Google Workspace
pack's OAuth redirect URI to match this domain.

To SSH in, each identity listed in `admin_members` runs:

```sh
gcloud compute ssh openneko --zone <zone> --project <project> --tunnel-through-iap
```

No SSH key setup needed -- OS Login plus the IAP tunnel IAM grant handle
both connectivity and auth. Revoking access later is a one-line Terraform
change: remove the identity from `admin_members` and `terraform apply`.

## Hardening notes

- **SSH exposure.** There is no public route to port 22 at all -- the only
  firewall rule for it is scoped to Google's fixed IAP relay range
  (`35.235.240.0/20`), and even that requires the caller to hold
  `roles/iap.tunnelResourceAccessor` on this instance (granted only to
  `admin_members`). Nothing here depends on a CIDR allowlist staying
  accurate over time.
- **End users are unaffected.** IAP only gates the admin/SSH path. The
  `allow_web` rule (80/443) is untouched and open to `0.0.0.0/0` -- anyone
  can reach the app on `https://<domain>` exactly as before.
- **Docker socket.** This VM is a real Docker host on purpose (that's the
  whole reason it isn't GKE) -- anyone who compromises a container with
  socket access effectively has root on the VM. Keep the surface area
  (SSH, any other exposed service) as small as this config already does.
- **Backups.** `openneko backup` (see INSTALL.md) writes one encrypted file
  on the VM. There is no backup service. This Terraform adds no off-VM backup
  storage (for example, a GCS bucket); add one if you need it.
- **Multiple customers on one VM.** If you plan to use OpenNeko's named
  multi-instance mode (`openneko --instance <name> setup ...`), this same
  VM/Caddy setup supports it -- just add one more `<domain> { reverse_proxy
  127.0.0.1:<port> }` block per instance to the Caddyfile and re-run
  `systemctl reload caddy`.
