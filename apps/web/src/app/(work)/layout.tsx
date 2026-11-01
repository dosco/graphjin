"use client";

import { usePathname } from "next/navigation";
import { WorkShellProvider } from "./work-shell-context";

export default function WorkShellLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // The context rail belongs only to Ask thread pages, not the
  // other surfaces (workflows/skills/memory) that share this shell.
  const pathname = usePathname();
  const isWork =
    pathname === "/work" ||
    pathname?.startsWith("/work/") ||
    pathname?.startsWith("/chat/t");
  const isWorkflows =
    pathname === "/workflows" || pathname?.startsWith("/workflows/");

  return (
    <WorkShellProvider>
      <div
        className={`root operations-root${isWork ? " is-work" : ""}${
          isWorkflows ? " is-workflows" : ""
        }`}
      >
        <div className="work-layout operations-layout">
          <main className="operations-main">{children}</main>
        </div>
      </div>
    </WorkShellProvider>
  );
}
