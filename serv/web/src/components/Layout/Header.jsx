import React from "react";
import { Network } from "lucide-react";

const Header = () => {
  return (
    <header className="gj-header">
      <div className="gj-header-brand">
        <div className="gj-logo" aria-hidden="true">
          <Network size={17} strokeWidth={1.75} />
        </div>
        <span className="gj-header-title">GraphJin Console</span>
      </div>
    </header>
  );
};

export default Header;
