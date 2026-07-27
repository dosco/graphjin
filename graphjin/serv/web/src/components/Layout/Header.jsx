import React from "react";
import { Menu, Network, UserRound, UserRoundCheck } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";

import Sidebar from "./Sidebar";
import {
  clearOperatorIdentity,
  hasOperatorIdentity,
  useOperatorIdentity,
  writeOperatorIdentity,
} from "../../services/identity";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";

const Header = () => {
  const queryClient = useQueryClient();
  const identity = useOperatorIdentity();
  const [identityOpen, setIdentityOpen] = React.useState(false);
  const [draft, setDraft] = React.useState(identity);

  React.useEffect(() => {
    if (!identityOpen) {
      setDraft(identity);
    }
  }, [identity, identityOpen]);

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
    <header className="sticky top-0 z-40 border-b bg-background shadow-[0_1px_0_rgba(255,255,255,0.9)]">
      <div className="flex h-16 items-center justify-between gap-4 px-4 md:px-6 lg:px-8">
        <div className="flex min-w-0 items-center gap-3">
          <Sheet>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="lg:hidden" aria-label="Open navigation">
                <Menu aria-hidden="true" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72 bg-background p-0">
              <SheetHeader className="border-b p-5">
                <SheetTitle>GraphJin Console</SheetTitle>
                <SheetDescription className="sr-only">Primary navigation</SheetDescription>
              </SheetHeader>
              <Sidebar className="border-0" />
            </SheetContent>
          </Sheet>
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-card text-foreground shadow-[0_1px_2px_rgba(28,35,48,0.06)]" aria-hidden="true">
            <Network size={17} strokeWidth={1.75} />
          </div>
          <div className="min-w-0">
            <span className="block truncate text-sm font-semibold leading-5 text-foreground">GraphJin Console</span>
            <span className="block truncate text-xs font-medium text-muted-foreground">Runtime control plane</span>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="hidden bg-card px-3 py-1.5 xl:inline-flex">agentic-ready</Badge>
          <Sheet open={identityOpen} onOpenChange={setIdentityOpen}>
            <SheetTrigger asChild>
              <Button type="button" variant="outline" size="sm">
                {hasOperatorIdentity(identity) ? <UserRoundCheck aria-hidden="true" /> : <UserRound aria-hidden="true" />}
                <span className="hidden sm:inline">{identity.userId || "Set identity"}</span>
                {identity.role && <Badge variant={identity.role === "admin" ? "warning" : "muted"}>{identity.role}</Badge>}
              </Button>
            </SheetTrigger>
            <SheetContent side="right" className="grid w-[min(92vw,26rem)] grid-rows-[auto_1fr] sm:w-[26rem]">
              <SheetHeader className="pr-8">
                <SheetTitle>Operator identity</SheetTitle>
                <SheetDescription>
                  Development headers for owner-scoped console data. Authenticated deployments still send their same-origin session cookies.
                </SheetDescription>
              </SheetHeader>
              <form className="grid content-start gap-4 overflow-y-auto py-5" onSubmit={saveIdentity}>
                <label className="grid gap-1.5 text-sm">
                  <span className="font-medium">User ID</span>
                  <Input
                    value={draft.userId}
                    onChange={(event) => setDraft((current) => ({ ...current, userId: event.target.value }))}
                    placeholder="operator-1"
                    autoComplete="off"
                  />
                </label>
                <label className="grid gap-1.5 text-sm">
                  <span className="font-medium">Role</span>
                  <Select
                    value={draft.role}
                    onChange={(event) => setDraft((current) => ({ ...current, role: event.target.value }))}
                  >
                    <option value="">Default role</option>
                    <option value="user">User</option>
                    <option value="operator">Operator</option>
                    <option value="admin">Admin</option>
                  </Select>
                </label>
                <label className="grid gap-1.5 text-sm">
                  <span className="font-medium">Account ID</span>
                  <Input
                    value={draft.accountId}
                    onChange={(event) => setDraft((current) => ({ ...current, accountId: event.target.value }))}
                    placeholder="account-1"
                    autoComplete="off"
                  />
                </label>
                <div className="rounded-lg border bg-muted/30 p-3 text-xs leading-5 text-muted-foreground">
                  These values are stored in this browser and sent as <code>X-User-ID</code>, <code>X-User-Role</code>, and <code>X-Account-ID</code>. Use only with development auth.
                </div>
                <div className="flex items-center justify-between gap-3">
                  <Button type="button" variant="ghost" onClick={clearIdentity} disabled={!hasOperatorIdentity(identity)}>
                    Clear
                  </Button>
                  <Button type="submit" disabled={!draft.userId.trim()}>Use identity</Button>
                </div>
              </form>
            </SheetContent>
          </Sheet>
        </div>
      </div>
    </header>
  );
};

export default Header;
