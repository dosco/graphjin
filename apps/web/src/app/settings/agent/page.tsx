import { redirect } from "next/navigation";

export default function SettingsAgentRedirectPage() {
  redirect("/admin/settings/agent");
}
