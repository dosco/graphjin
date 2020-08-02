import React from "react";
import classNames from "classnames";

export default function Button({ to, children, className, target }) {
  return (
    <a
      href={to}
      target={target || "_self"}
      className={classNames(
        "hover:text-white button_pink rounded-lg px-4 md:px-5 xl:px-4 py-3 md:py-4 xl:py-3 bg-pink-500 hover:bg-pink-600 md:text-lg xl:text-base text-white font-semibold leading-tight shadow-md",
        className
      )}
    >
      {children}
    </a>
  );
}
