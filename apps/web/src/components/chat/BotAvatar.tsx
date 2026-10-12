"use client";

import { NekoAvatar, nekoColorsFor, type NekoAvatarProps, type NekoPattern } from "@neko/avatars/react";

export function BotAvatar({
  coat,
  pattern,
  ...rest
}: Omit<NekoAvatarProps, "seed" | "colors"> & { coat: string; pattern: NekoPattern }) {
  return <NekoAvatar seed="bot" colors={nekoColorsFor("bot", coat, pattern)} {...rest} />;
}
