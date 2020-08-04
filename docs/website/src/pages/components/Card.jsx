import React from "react";
import classNames from "classnames";

export default function Card({ title, description }) {
  return (
    <div className="first:pl-0 p-4 w-full md:w-1/3 prose-lg">
      <h4 className="font-semibold">{title}</h4>
      <p>{description}</p>
    </div>
  );
}
