terraform {
  required_version = ">= 1.5.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

# Static IP so the domain's A record survives instance rebuilds.
resource "google_compute_address" "openneko" {
  name   = "${var.name}-ip"
  region = var.region
}

resource "google_service_account" "openneko" {
  account_id   = "${var.name}-vm"
  display_name = "OpenNeko VM service account"
}

# 80 (ACME challenge + HTTP->HTTPS redirect) and 443 (the app, via Caddy) stay
# open to the internet -- end users reach OpenNeko directly on 443, no IAP
# involved. OpenNeko's own web port (3000) stays bound to 127.0.0.1 and is
# never reachable from this firewall's allow rules.
resource "google_compute_firewall" "allow_web" {
  name    = "${var.name}-allow-web"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["${var.name}-vm"]
}

# SSH is reachable ONLY through IAP's TCP-forwarding relay -- 35.235.240.0/20
# is Google's fixed, non-internet-routable IAP source range, never a public
# CIDR. There is no rule here opening 22 to 0.0.0.0/0 or to any admin IP.
resource "google_compute_firewall" "allow_iap_ssh" {
  name    = "${var.name}-allow-iap-ssh"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["35.235.240.0/20"]
  target_tags   = ["${var.name}-vm"]
}

# Per-instance grant: only these identities can open an IAP tunnel to this
# VM at all, regardless of source IP.
resource "google_iap_tunnel_instance_iam_member" "ssh_tunnel" {
  for_each = toset(var.admin_members)
  project  = var.project_id
  zone     = var.zone
  instance = google_compute_instance.openneko.name
  role     = "roles/iap.tunnelResourceAccessor"
  member   = each.value
}

# OS Login: identity-based SSH via the same IAM members, no manual SSH-key
# metadata to manage or rotate. osAdminLogin (not the plain osLogin role)
# grants passwordless sudo, matching the `sudo -i` step in the next_steps
# output below.
resource "google_project_iam_member" "os_admin_login" {
  for_each = toset(var.admin_members)
  project  = var.project_id
  role     = "roles/compute.osAdminLogin"
  member   = each.value
}

resource "google_project_service" "iap" {
  count              = var.manage_apis ? 1 : 0
  project            = var.project_id
  service            = "iap.googleapis.com"
  disable_on_destroy = false
}

resource "google_compute_instance" "openneko" {
  name         = var.name
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["${var.name}-vm"]

  boot_disk {
    initialize_params {
      image = var.boot_image
      size  = var.boot_disk_size_gb
      type  = "pd-ssd"
    }
  }

  network_interface {
    network = var.network
    access_config {
      nat_ip = google_compute_address.openneko.address
    }
  }

  service_account {
    email  = google_service_account.openneko.email
    scopes = ["cloud-platform"]
  }

  metadata = {
    enable-oslogin = "TRUE"
    startup-script = templatefile("${path.module}/startup-script.sh.tftpl", {
      domain    = var.domain
      tls_email = var.tls_email
    })
  }

  allow_stopping_for_update = true
}

# Optional: only created if you manage the domain's zone in Cloud DNS.
# Otherwise, point the domain's A record at google_compute_address.openneko
# with your own registrar/DNS provider (see the `next_steps` output).
resource "google_dns_record_set" "openneko" {
  count        = var.manage_dns ? 1 : 0
  name         = "${var.domain}."
  type         = "A"
  ttl          = 300
  managed_zone = var.dns_managed_zone
  rrdatas      = [google_compute_address.openneko.address]
}
