import React from "react";

export default function Card({ title, description }) {
  return (
    <div>
      <h4 className="text-2xl mb-0">{title}</h4>
      <p className="text-lg">{description}</p>
    </div>
  );
}
