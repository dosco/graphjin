import { redirect } from "next/navigation";

export default function SettingsRulesRedirectPage() {
  redirect("/admin/rules");
}
