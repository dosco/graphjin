import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  Check,
  ChevronDown,
  Command as CommandIcon,
  Moon,
  Network,
  Search,
  Sun,
  UserRound,
  UserRoundCheck,
} from "lucide-react";

import { availableWorkspaces, commandItems, workspaceDefinitions, workspaceIDFromPath } from "@/app/workspaces";
import { cn } from "@/lib/utils";
import {
  clearOperatorIdentity,
  hasOperatorIdentity,
  useOperatorIdentity,
  writeOperatorIdentity,
} from "@/services/identity";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ButtonGroup } from "@/components/ui/button-group";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { useTheme } from "@/components/theme-provider";
import { useConsole } from "./console-context";

const lastRouteKey = (workspaceID) => `graphjin-console-last-route:${workspaceID}`;

const Header = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const { bootstrap } = useConsole();
  const { theme, setTheme } = useTheme();
  const identity = useOperatorIdentity();
  const [identityOpen, setIdentityOpen] = React.useState(false);
  const [commandOpen, setCommandOpen] = React.useState(false);
  const [draft, setDraft] = React.useState(identity);
  const workspaceID = workspaceIDFromPath(location.pathname);
  const workspace = workspaceDefinitions[workspaceID];
  const workspaces = availableWorkspaces(bootstrap);
  const scopeLabel = bootstrap?.scope?.environment || bootstrap?.scope?.source || "local";

  React.useEffect(() => {
    if (workspaceDefinitions[workspaceID]) {
      localStorage.setItem(lastRouteKey(workspaceID), `${location.pathname}${location.search}`);
    }
  }, [location.pathname, location.search, workspaceID]);

  React.useEffect(() => {
    if (!identityOpen) {
      setDraft(identity);
    }
  }, [identity, identityOpen]);

  React.useEffect(() => {
    const handleKeyDown = (event) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  function switchWorkspace(target) {
    const stored = localStorage.getItem(lastRouteKey(target.id));
    navigate(stored?.startsWith(`/${target.id}/`) ? stored : target.defaultPath);
  }

  function selectCommand(path) {
    navigate(path);
    setCommandOpen(false);
  }

  function saveIdentity(event) {
    event.preventDefault();
    if (!draft.userId.trim()) {
      return;
    }
    writeOperatorIdentity(draft);
    void queryClient.invalidateQueries();
    setIdentityOpen(false);
  }

  function clearIdentity() {
    clearOperatorIdentity();
    void queryClient.invalidateQueries();
    setIdentityOpen(false);
  }

  return (
    <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/88">
      <div className="flex h-14 items-center gap-3 px-3 sm:px-5 lg:px-6">
        <Link to={workspace?.defaultPath || "/user/agent"} className="flex min-w-0 shrink-0 items-center gap-2.5" aria-label="GraphJin Console home">
          <span className="flex size-8 items-center justify-center rounded-lg bg-foreground text-background" aria-hidden="true">
            <Network size={16} strokeWidth={1.8} />
          </span>
          <span className="hidden text-sm font-semibold tracking-tight sm:block">GraphJin</span>
        </Link>

        <ButtonGroup className="ml-1" aria-label="Workspace">
          {workspaces.map((item) => (
            <Button
              key={item.id}
              type="button"
              size="sm"
              variant={item.id === workspaceID ? "secondary" : "ghost"}
              className={cn("border", item.id !== workspaceID && "border-transparent")}
              aria-current={item.id === workspaceID ? "page" : undefined}
              onClick={() => switchWorkspace(item)}
            >
              {item.label}
            </Button>
          ))}
        </ButtonGroup>

        <Badge variant="muted" className="hidden max-w-40 truncate md:inline-flex" title={scopeLabel}>
          {scopeLabel}
        </Badge>

        <div className="ml-auto flex items-center gap-1.5">
          <Button type="button" variant="outline" size="sm" className="hidden min-w-44 justify-start text-muted-foreground md:inline-flex" onClick={() => setCommandOpen(true)}>
            <Search aria-hidden="true" />
            Search console
            <kbd className="ml-auto rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
          </Button>
          <Button type="button" variant="ghost" size="icon-sm" className="md:hidden" onClick={() => setCommandOpen(true)} aria-label="Search console">
            <Search aria-hidden="true" />
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="ghost" size="icon-sm" aria-label="Change theme">
                {theme === "dark" ? <Moon /> : <Sun />}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Appearance</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={theme} onValueChange={setTheme}>
                <DropdownMenuRadioItem value="light">Light</DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="dark">Dark</DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="system">System</DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>

          <Sheet open={identityOpen} onOpenChange={setIdentityOpen}>
            <SheetTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="max-w-48"
                aria-label={hasOperatorIdentity(identity) ? `Identity: ${identity.userId}` : "Set operator identity"}
              >
                {hasOperatorIdentity(identity) ? <UserRoundCheck aria-hidden="true" /> : <UserRound aria-hidden="true" />}
                <span className="hidden max-w-28 truncate lg:inline">{identity.userId || bootstrap?.identity?.role || "Identity"}</span>
                <ChevronDown className="hidden size-3 lg:block" aria-hidden="true" />
              </Button>
            </SheetTrigger>
            <SheetContent side="right" className="grid w-[min(92vw,26rem)] grid-rows-[auto_1fr] sm:w-[26rem]">
              <SheetHeader className="pr-8">
                <SheetTitle>Operator identity</SheetTitle>
                <SheetDescription>Development headers for owner-scoped console data. Authenticated deployments continue using their same-origin session.</SheetDescription>
              </SheetHeader>
              <form className="grid content-start gap-4 overflow-y-auto py-5" onSubmit={saveIdentity}>
                <label className="grid gap-1.5 text-sm"><span className="font-medium">User ID</span><Input value={draft.userId} onChange={(event) => setDraft((current) => ({ ...current, userId: event.target.value }))} placeholder="operator-1" autoComplete="off" /></label>
                <label className="grid gap-1.5 text-sm"><span className="font-medium">Role</span><Select value={draft.role} onChange={(event) => setDraft((current) => ({ ...current, role: event.target.value }))}><option value="">Default role</option><option value="user">User</option><option value="operator">Operator</option><option value="admin">Admin</option></Select></label>
                <label className="grid gap-1.5 text-sm"><span className="font-medium">Account ID</span><Input value={draft.accountId} onChange={(event) => setDraft((current) => ({ ...current, accountId: event.target.value }))} placeholder="account-1" autoComplete="off" /></label>
                <div className="rounded-lg bg-muted p-3 text-xs leading-5 text-muted-foreground">Stored in this browser and sent only as development identity headers.</div>
                <div className="flex items-center justify-between gap-3"><Button type="button" variant="ghost" onClick={clearIdentity} disabled={!hasOperatorIdentity(identity)}>Clear</Button><Button type="submit" disabled={!draft.userId.trim()}>Use identity</Button></div>
              </form>
            </SheetContent>
          </Sheet>
        </div>
      </div>

      <nav className="scrollbar-none flex h-11 items-center gap-1 overflow-x-auto border-t px-3 sm:px-5 lg:px-6" aria-label={`${workspace?.label || "Console"} workspace`}>
        <span className="mr-2 hidden shrink-0 text-xs font-medium text-muted-foreground sm:inline">{workspace?.description}</span>
        {workspace?.items.map((item) => {
          const active = location.pathname.startsWith(item.path);
          return (
            <Link
              key={item.path}
              to={item.path}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative inline-flex h-10 shrink-0 items-center gap-2 px-2.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
                active && "text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-foreground"
              )}
            >
              <item.icon className="size-4" aria-hidden="true" />
              {item.label}
            </Link>
          );
        })}
      </nav>

      <CommandDialog open={commandOpen} onOpenChange={setCommandOpen}>
        <CommandInput placeholder="Search views and actions…" autoFocus />
        <CommandList>
          <CommandEmpty>No matching console view.</CommandEmpty>
          {workspaces.map((item) => (
            <CommandGroup key={item.id} heading={item.label}>
              {commandItems(bootstrap).filter((command) => command.workspace.id === item.id).map((command) => (
                <CommandItem key={command.path} value={`${item.label} ${command.label}`} onSelect={() => selectCommand(command.path)}>
                  <command.icon aria-hidden="true" />
                  <span>{command.label}</span>
                  {location.pathname.startsWith(command.path) && <Check className="ml-auto" aria-hidden="true" />}
                </CommandItem>
              ))}
            </CommandGroup>
          ))}
          <CommandGroup heading="Actions">
            <CommandItem onSelect={() => { setCommandOpen(false); setIdentityOpen(true); }}><UserRound aria-hidden="true" />Identity<CommandShortcut>Settings</CommandShortcut></CommandItem>
            <CommandItem onSelect={() => { setTheme(theme === "dark" ? "light" : "dark"); setCommandOpen(false); }}><CommandIcon aria-hidden="true" />Toggle theme<CommandShortcut>{theme}</CommandShortcut></CommandItem>
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </header>
  );
};

export default Header;
