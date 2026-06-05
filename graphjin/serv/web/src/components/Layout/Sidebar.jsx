import React from "react";
import { NavLink } from "react-router-dom";
import { Code, FileText, LayoutDashboard, Library, Shield, SlidersHorizontal, SquareTerminal } from "lucide-react";

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

const Sidebar = () => {
  return (
    <nav className="gj-sidebar">
      <ul className="gj-sidebar-nav">
        {navItems.map((item) => (
          <li key={item.path}>
            <NavLink
              to={item.path}
              className={({ isActive }) =>
                `gj-sidebar-link ${isActive ? "active" : ""}`
              }
              end={item.path === "/"}
              title={item.label}
            >
              <span className="gj-sidebar-icon">
                <item.icon size={17} strokeWidth={1.75} aria-hidden="true" />
              </span>
              <span className="gj-sidebar-label">{item.label}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
};

export default Sidebar;
