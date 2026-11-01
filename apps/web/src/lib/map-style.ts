/**
 * The map tile style for answer maps, from OPENNEKO_MAP_STYLE: unset uses
 * OpenFreeMap, "off" shows places as a list with no outside request, and an
 * http(s) URL points at a self-hosted MapLibre style.
 */
export function mapStyleSetting(): string | undefined {
  const value = process.env.OPENNEKO_MAP_STYLE?.trim();
  if (!value) return undefined;
  if (value === "off") return "off";
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? url.toString() : undefined;
  } catch {
    return undefined;
  }
}
