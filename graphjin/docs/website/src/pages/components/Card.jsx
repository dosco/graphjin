import React from "react";
import classNames from "classnames";

export default function Card({ title, description }) {
  return (
    <div className="p-4 w-full md:w-1/3 prose lg:prose-lg">
      <h4>{title}</h4>
      <p>{description}</p>
    </div>
  );
}
