import React from "react";
import { Menu, Network } from "lucide-react";

import Sidebar from "./Sidebar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";

const Header = () => {
  return (
    <header className="sticky top-0 z-40 border-b bg-background/90 backdrop-blur supports-[backdrop-filter]:bg-background/75">
      <div className="flex h-16 items-center justify-between gap-4 px-4 md:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <Sheet>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="lg:hidden" aria-label="Open navigation">
                <Menu aria-hidden="true" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72 p-0">
              <SheetHeader className="border-b p-5">
                <SheetTitle>GraphJin Console</SheetTitle>
              </SheetHeader>
              <Sidebar className="border-0" />
            </SheetContent>
          </Sheet>
          <div className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-card text-foreground shadow-xs" aria-hidden="true">
            <Network size={17} strokeWidth={1.75} />
          </div>
          <div className="min-w-0">
            <span className="block truncate text-sm font-semibold leading-5 text-foreground">GraphJin Console</span>
            <span className="block truncate text-xs text-muted-foreground">Runtime control plane</span>
          </div>
        </div>
        <Badge variant="outline" className="hidden md:inline-flex">agentic-ready</Badge>
      </div>
    </header>
  );
};

export default Header;
