variable "project_id" {
  description = "GCP project ID to deploy into"
  type        = string
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "zone" {
  type    = string
  default = "us-central1-a"
}

variable "name" {
  description = "Base name for all resources (VM, IP, firewall rules, service account)"
  type        = string
  default     = "openneko"
}

variable "machine_type" {
  description = <<-EOT
    OpenNeko's "prod" mode runs ~10 containers on one Docker daemon: web,
    worker, two Postgres instances, three internal GraphJin instances, the
    customer-facing GraphJin, and the agent runtime (openneko serve and
    sandbox), whose terminal commands share the VM. e2-standard-4 (4 vCPU /
    16GB) is a reasonable floor; size up if you run several named instances on
    one VM or expect concurrent agent load.
  EOT
  type    = string
  default = "e2-standard-4"
}

variable "boot_image" {
  type    = string
  default = "ubuntu-os-cloud/ubuntu-2204-lts"
}

variable "boot_disk_size_gb" {
  type    = number
  default = 100
}

variable "network" {
  type    = string
  default = "default"
}

variable "admin_members" {
  description = <<-EOT
    IAM members granted SSH access to the VM -- via an IAP tunnel (no open
    port 22, no CIDR allowlist) and OS Login (no SSH-key metadata to manage).
    Use the IAM member format, e.g. "user:name@example.com" or
    "group:ops@example.com". Defaults to empty -- this is a checked-in,
    shared config, so no identity is granted access out of the box. Set
    this in your own untracked terraform.tfvars (see .gitignore), never in
    a file meant to be committed.
  EOT
  type    = list(string)
  default = []
}

variable "manage_apis" {
  description = "Set false if iap.googleapis.com is already enabled on this project by other means"
  type        = bool
  default     = true
}

variable "domain" {
  description = "Public domain name pointed at this VM, e.g. neko.example.com. Also becomes the plugin OAuth redirect host and OPENNEKO_PUBLIC_URL."
  type        = string
}

variable "tls_email" {
  description = "Email Caddy registers with Let's Encrypt for certificate issuance/renewal notices"
  type        = string
}

variable "manage_dns" {
  description = "Set true to have Terraform create the A record in a Cloud DNS managed zone you already own"
  type        = bool
  default     = false
}

variable "dns_managed_zone" {
  description = "Cloud DNS managed zone name. Required if manage_dns = true."
  type        = string
  default     = null
}
