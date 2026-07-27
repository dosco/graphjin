import React from "react";
import { Link, useLocation } from "react-router-dom";
import { Bot, Code, FileText, LayoutDashboard, Library, Radar, Shield, SlidersHorizontal, SquareTerminal } from "lucide-react";

import { cn } from "@/lib/utils";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const navItems = [
  {
    path: "/",
    label: "Runtime",
    icon: LayoutDashboard,
  },
  {
    path: "/workbench",
    label: "Workbench",
    icon: SquareTerminal,
  },
  {
    path: "/agent",
    label: "Agent",
    icon: Bot,
  },
  {
    path: "/mission",
    label: "Mission Control",
    icon: Radar,
  },
  {
    path: "/catalog",
    label: "Catalog",
    icon: Library,
  },
  {
    path: "/security",
    label: "Security",
    icon: Shield,
  },
  {
    path: "/code",
    label: "Code",
    icon: Code,
  },
  {
    path: "/config",
    label: "Config",
    icon: SlidersHorizontal,
  },
  {
    path: "/api-docs",
    label: "API Docs",
    icon: FileText,
  },
];

const Sidebar = ({ className }) => {
  const location = useLocation();
  return (
    <nav className={cn("border-r bg-background p-3", className)} aria-label="Primary">
      <TooltipProvider delayDuration={250}>
        <ul className="grid gap-1.5">
          {navItems.map((item) => {
            const isActive = item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path);
            return (
              <li key={item.path}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Link
                      to={item.path}
                      className={cn(
                        "flex h-10 items-center gap-3 rounded-lg px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
                        isActive && "border bg-card text-foreground shadow-[0_1px_2px_rgba(28,35,48,0.06)]"
                      )}
                    >
                      <span className={cn("flex size-6 items-center justify-center rounded-md", isActive ? "bg-muted text-foreground" : "text-muted-foreground")}>
                        <item.icon size={16} strokeWidth={1.8} aria-hidden="true" />
                      </span>
                      <span className="truncate">{item.label}</span>
                    </Link>
                  </TooltipTrigger>
                  <TooltipContent side="right">{item.label}</TooltipContent>
                </Tooltip>
              </li>
            );
          })}
        </ul>
      </TooltipProvider>
    </nav>
  );
};

export default Sidebar;
