import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  Check,
  Search,
} from "lucide-react";

import { availableWorkspaces, commandItems, workspaceDefinitions, workspaceIDFromPath } from "@/app/workspaces";
import { cn } from "@/lib/utils";
import {
  clearOperatorIdentity,
  hasOperatorIdentity,
  useOperatorIdentity,
  writeOperatorIdentity,
} from "@/services/identity";
import { Button } from "@/components/ui/button";
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
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { useTheme } from "@/components/theme-provider";
import { signInState } from "@/services/session";
import { useConsole } from "./console-context";
import SignInPanel from "./SignInPanel";

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
  const session = signInState(bootstrap);
  const promptedSignIn = React.useRef(false);

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

  // A JWT-only server shows nothing until the console has a session, so ask
  // once instead of leaving every page to fail with 401.
  React.useEffect(() => {
    if (session.required && !session.signedIn && !promptedSignIn.current) {
      promptedSignIn.current = true;
      setIdentityOpen(true);
    }
  }, [session.required, session.signedIn]);

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

  function sessionChanged() {
    // Every cached query belongs to the previous identity.
    queryClient.clear();
    window.location.reload();
  }

  function clearIdentity() {
    clearOperatorIdentity();
    void queryClient.invalidateQueries();
    setIdentityOpen(false);
  }

  const identityLabel = session.required
    ? (session.signedIn ? session.name : "Sign in")
    : (identity.userId || bootstrap?.identity?.role || "Identity");
  const identityDetail = [
    session.required ? (session.signedIn ? session.role || bootstrap?.identity?.role : "signed out") : (identity.role || bootstrap?.identity?.role),
    scopeLabel,
  ].filter(Boolean).join(" · ");

  // One rail serves every width: a top bar with a bottom tab bar on phones,
  // and a labelled rail from md up. Type carries the structure; the accent
  // only marks where you are.
  return (
    <header className="sticky top-0 z-40 flex h-14 shrink-0 items-center gap-3 px-4 md:h-dvh md:w-56 md:flex-col md:items-stretch md:gap-0 md:self-start md:px-4 md:py-6 xl:w-64 xl:px-6">
      <div aria-hidden="true" className="absolute inset-0 -z-10 bg-background/85 backdrop-blur-xl md:hidden" />

      <Link
        to={workspace?.defaultPath || "/user/agent"}
        className="flex shrink-0 items-baseline gap-2 md:px-2"
        aria-label="GraphJin Console home"
      >
        <span className="text-[1.0625rem] font-extrabold tracking-[-0.02em]">GraphJin</span>
        <span className="hud-label hidden sm:inline">Console</span>
      </Link>

      <div role="group" aria-label="Workspace" className="flex items-center gap-4 md:mt-8 md:px-2">
        {workspaces.map((item) => {
          const current = item.id === workspaceID;
          return (
            <button
              key={item.id}
              type="button"
              aria-current={current ? "page" : undefined}
              title={item.description}
              onClick={() => switchWorkspace(item)}
              className={cn(
                "relative py-1 font-mono text-[0.6875rem] font-medium uppercase tracking-[0.12em] transition-colors",
                "after:absolute after:inset-x-0 after:-bottom-0.5 after:h-px after:bg-transparent",
                current ? "text-foreground after:bg-brand" : "text-muted-foreground hover:text-foreground"
              )}
            >
              {item.label}
            </button>
          );
        })}
      </div>

      <nav
        className="scrollbar-none fixed inset-x-0 bottom-0 z-40 flex h-[var(--chrome-bottom)] items-stretch overflow-x-auto bg-background/85 px-2 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl md:static md:mt-6 md:h-auto md:flex-col md:gap-0.5 md:overflow-visible md:bg-transparent md:p-0 md:backdrop-blur-none"
        aria-label={`${workspace?.label || "Console"} workspace`}
      >
        {workspace?.items.map((item, index) => {
          const active = location.pathname.startsWith(item.path);
          return (
            <Link
              key={item.path}
              to={item.path}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative flex flex-1 shrink-0 items-center justify-center px-3 text-[0.8125rem] font-semibold text-muted-foreground transition-colors hover:text-foreground",
                "before:absolute before:inset-x-4 before:bottom-3.5 before:h-px before:bg-transparent",
                "md:flex-none md:justify-start md:gap-3 md:px-2 md:py-2 md:text-[1.0625rem] md:tracking-[-0.01em]",
                "md:before:inset-x-auto md:before:inset-y-2.5 md:before:-left-4 md:before:h-auto md:before:w-px xl:before:-left-6",
                active && "text-foreground before:bg-brand md:font-bold"
              )}
            >
              <span aria-hidden="true" className="hidden w-5 font-mono text-[0.6875rem] font-normal text-muted-foreground md:inline">
                {String(index + 1).padStart(2, "0")}
              </span>
              {item.label}
            </Link>
          );
        })}
      </nav>

      <div className="ml-auto flex items-center gap-1 md:ml-0 md:mt-auto md:flex-col md:items-stretch md:gap-3">
        <button
          type="button"
          onClick={() => setCommandOpen(true)}
          className="flex items-center gap-2 py-1 text-sm text-muted-foreground transition-colors hover:text-foreground md:px-2"
          aria-label="Search console"
        >
          <Search className="size-4 md:hidden" aria-hidden="true" />
          <span className="hidden md:inline">Search</span>
          <kbd aria-hidden="true" className="ml-auto hidden font-mono text-[0.6875rem] md:inline">⌘K</kbd>
        </button>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="hidden items-center gap-2 py-1 text-sm text-muted-foreground transition-colors hover:text-foreground md:flex md:px-2"
              aria-label="Change theme"
            >
              Appearance
              <span className="ml-auto font-mono text-[0.6875rem]">{theme}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" side="top">
            <DropdownMenuLabel>Appearance</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={theme} onValueChange={setTheme}>
              <DropdownMenuRadioItem value="dark">Dark</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="light">Light</DropdownMenuRadioItem>
              <DropdownMenuRadioItem value="system">System</DropdownMenuRadioItem>
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <Sheet open={identityOpen} onOpenChange={setIdentityOpen}>
          <SheetTrigger asChild>
            <button
              type="button"
              className="grid min-w-0 py-1 pl-2 text-left transition-colors md:px-2 md:pt-2 [&:hover_.identity-name]:text-brand"
              aria-label={session.required
                ? (session.signedIn ? `Signed in as ${session.name}` : "Sign in")
                : (hasOperatorIdentity(identity) ? `Identity: ${identity.userId}` : "Set operator identity")}
            >
              <span className="identity-name max-w-28 truncate text-sm font-bold transition-colors md:max-w-none">{identityLabel}</span>
              <span className="hidden truncate font-mono text-[0.6875rem] text-muted-foreground md:block">{identityDetail}</span>
            </button>
          </SheetTrigger>
          <SheetContent side="right" className="grid w-[min(92vw,26rem)] grid-rows-[auto_1fr] sm:w-[26rem]">
            <SheetHeader className="pr-8">
              <SheetTitle>{session.required ? (session.signedIn ? "Session" : "Sign in") : "Operator identity"}</SheetTitle>
              <SheetDescription>
                {session.required
                  ? "Your console session on this server."
                  : "Development headers for owner-scoped console data. Authenticated deployments continue using their same-origin session."}
              </SheetDescription>
            </SheetHeader>
            {session.required ? <SignInPanel state={session} onChange={sessionChanged} /> : <form className="grid content-start gap-4 overflow-y-auto py-5" onSubmit={saveIdentity}>
              <label className="grid gap-1.5 text-sm"><span className="font-medium">User ID</span><Input value={draft.userId} onChange={(event) => setDraft((current) => ({ ...current, userId: event.target.value }))} placeholder="operator-1" autoComplete="off" /></label>
              <label className="grid gap-1.5 text-sm"><span className="font-medium">Role</span><Select value={draft.role} onChange={(event) => setDraft((current) => ({ ...current, role: event.target.value }))}><option value="">Default role</option><option value="user">User</option><option value="operator">Operator</option><option value="admin">Admin</option></Select></label>
              <label className="grid gap-1.5 text-sm"><span className="font-medium">Account ID</span><Input value={draft.accountId} onChange={(event) => setDraft((current) => ({ ...current, accountId: event.target.value }))} placeholder="account-1" autoComplete="off" /></label>
              <p className="text-xs leading-5 text-muted-foreground">Stored in this browser and sent only as development identity headers.</p>
              <div className="flex items-center justify-between gap-3"><Button type="button" variant="ghost" onClick={clearIdentity} disabled={!hasOperatorIdentity(identity)}>Clear</Button><Button type="submit" disabled={!draft.userId.trim()}>Use identity</Button></div>
            </form>}
            {/* Phones have no rail footer, so appearance lives in the session sheet there. */}
            <div className="flex items-center gap-4 self-end border-t pt-4 md:hidden">
              <span className="hud-label">Appearance</span>
              {["dark", "light", "system"].map((value) => (
                <button key={value} type="button" onClick={() => setTheme(value)} className={cn("font-mono text-xs", theme === value ? "text-foreground underline decoration-brand underline-offset-4" : "text-muted-foreground")}>
                  {value}
                </button>
              ))}
            </div>
          </SheetContent>
        </Sheet>
      </div>

      <CommandDialog open={commandOpen} onOpenChange={setCommandOpen}>
        <CommandInput placeholder="Search views and actions…" autoFocus />
        <CommandList>
          <CommandEmpty>No matching console view.</CommandEmpty>
          {workspaces.map((item) => (
            <CommandGroup key={item.id} heading={item.label}>
              {commandItems(bootstrap).filter((command) => command.workspace.id === item.id).map((command) => (
                <CommandItem key={command.path} value={`${item.label} ${command.label}`} onSelect={() => selectCommand(command.path)}>
                  <span>{command.label}</span>
                  {location.pathname.startsWith(command.path) && <Check className="ml-auto" aria-hidden="true" />}
                </CommandItem>
              ))}
            </CommandGroup>
          ))}
          <CommandGroup heading="Actions">
            <CommandItem onSelect={() => { setCommandOpen(false); setIdentityOpen(true); }}>Identity<CommandShortcut>Settings</CommandShortcut></CommandItem>
            <CommandItem onSelect={() => { setTheme(theme === "dark" ? "light" : "dark"); setCommandOpen(false); }}>Toggle theme<CommandShortcut>{theme}</CommandShortcut></CommandItem>
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </header>
  );
};

export default Header;
