import { redirect } from "next/navigation";

export default function SettingsSecurityRedirectPage() {
  redirect("/admin/settings/security");
}
