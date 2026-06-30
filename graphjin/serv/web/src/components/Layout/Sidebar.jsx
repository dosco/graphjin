import React from "react";
import { Link, useLocation } from "react-router-dom";
import { Bot, Code, FileText, LayoutDashboard, Library, Shield, SlidersHorizontal, SquareTerminal } from "lucide-react";

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
    <nav className={cn("border-r bg-muted/30 p-3", className)} aria-label="Primary">
      <TooltipProvider delayDuration={250}>
        <ul className="grid gap-1">
          {navItems.map((item) => {
            const isActive = item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path);
            return (
              <li key={item.path}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Link
                      to={item.path}
                      className={cn(
                        "flex h-10 items-center gap-3 rounded-md px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground",
                        isActive && "bg-background text-foreground shadow-xs ring-1 ring-border"
                      )}
                    >
                      <item.icon size={17} strokeWidth={1.75} aria-hidden="true" />
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
