import React from "react";

const Header = () => {
  return (
    <header className="gj-header">
      <div className="gj-header-brand">
        <svg
          className="gj-logo"
          viewBox="0 0 32 32"
          width="28"
          height="28"
          fill="none"
          role="img"
          aria-label="GraphJin logo"
        >
          <circle cx="16" cy="16" r="14" stroke="currentColor" strokeWidth="2" />
          <path
            d="M10 16h12M16 10v12M12 12l8 8M20 12l-8 8"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
          />
        </svg>
        <span className="gj-header-title">GraphJin</span>
        <span className="gj-header-subtitle">Admin</span>
      </div>
      <div className="gj-header-actions">
        <span className="gj-header-badge gj-header-env">Dev</span>
      </div>
    </header>
  );
};

export default Header;
