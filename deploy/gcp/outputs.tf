output "external_ip" {
  value       = google_compute_address.openneko.address
  description = "Point the domain's DNS A record at this IP (skip if manage_dns = true)"
}

output "ssh_command" {
  value       = "gcloud compute ssh ${google_compute_instance.openneko.name} --zone ${var.zone} --project ${var.project_id} --tunnel-through-iap"
  description = "No public SSH port exists -- this tunnels through IAP. Only identities listed in admin_members can run it."
}

output "next_steps" {
  value = <<-EOT
    1. DNS: point ${var.domain}'s A record at ${google_compute_address.openneko.address}
       (skip this if manage_dns = true -- Terraform already created it).
       This is the ONLY public entry point end users need -- 443 is open to
       everyone, no IAP involved for them.

    2. Wait for the startup script to finish (~3-5 min), then confirm via the
       IAP tunnel (port 22 has no public route -- this is the only way in):
         gcloud compute ssh ${var.name} --zone ${var.zone} --project ${var.project_id} --tunnel-through-iap
         sudo journalctl -u google-startup-scripts -f
       Caddy should already be serving https://${var.domain} once DNS propagates
       and it completes the ACME challenge on port 80.

    3. Run the interactive OpenNeko installer on the VM:
         sudo -i
         cd /opt/openneko
         openneko setup --mode prod --port 3000 --bind-address 127.0.0.1
       Answer the wizard (admin password, data source, model provider + key).
       For unattended installs, pass --admin-password/--provider/--provider-key
       instead -- see INSTALL.md's "Command reference".

    4. If you're installing the Google Workspace pack, set its OAuth
       client's Authorized redirect URI to:
         https://${var.domain}/api/pack-accounts/google-workspace/workspace/callback
       OPENNEKO_PUBLIC_URL is already set to https://${var.domain} on the VM
       (see /etc/environment) so OpenNeko's own callback construction matches.
  EOT
}
