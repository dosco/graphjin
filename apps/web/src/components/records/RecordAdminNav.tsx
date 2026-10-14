import Link from "next/link";
import { Columns3, FileClock, FileUp, ShieldCheck, UsersRound } from "lucide-react";

const ITEMS = [
  { key: "fields", path: "/fields", label: "Fields", icon: Columns3 },
  { key: "import", path: "", label: "Import", icon: FileUp },
  { key: "identity", path: "/identity", label: "Identity", icon: UsersRound },
  { key: "permissions", path: "/permissions", label: "Permissions", icon: ShieldCheck },
  { key: "history", path: "/history", label: "History", icon: FileClock },
] as const;

export function RecordAdminNav({ appId, active }: { appId: string; active: typeof ITEMS[number]["key"] }) {
  return (
    <nav className="records-admin-nav" aria-label="App administration">
      {ITEMS.map((item) => {
        const Icon = item.icon;
        return (
          <Link
            className={active === item.key ? "is-active" : undefined}
            href={`/a/${appId}/admin${item.path}`}
            key={item.key}
          >
            <Icon aria-hidden="true" /> {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
