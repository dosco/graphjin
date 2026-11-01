"use client";

import { useEffect, useState } from "react";

type LocalDateTimeProps = {
  value: string | Date;
  fallback?: string;
  className?: string;
  relativeDay?: boolean;
};

function formatLocal(date: Date, relativeDay: boolean) {
  const now = new Date();
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  const day = date.toDateString();
  const label = day === now.toDateString() ? "Today" : day === yesterday.toDateString() ? "Yesterday" : null;
  if (relativeDay && label) {
    return `${label} at ${new Intl.DateTimeFormat(undefined, { timeStyle: "short" }).format(date)}`;
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

export function LocalDateTime({
  value,
  fallback = "—",
  className,
  relativeDay = false,
}: LocalDateTimeProps) {
  const iso = value instanceof Date ? value.toISOString() : value;
  const [formatted, setFormatted] = useState<string | null>(null);

  useEffect(() => {
    const id = window.setTimeout(() => {
      setFormatted(formatLocal(new Date(iso), relativeDay));
    }, 0);
    return () => window.clearTimeout(id);
  }, [iso, relativeDay]);

  return (
    <time dateTime={iso} className={className}>
      {formatted ?? fallback}
    </time>
  );
}
