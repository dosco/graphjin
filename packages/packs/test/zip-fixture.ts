import { crc32, deflateRawSync } from "node:zlib";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";

export type ZipEntry = { name: string; data?: string | Buffer; mode?: number; deflate?: boolean; size?: number; crc?: number; flags?: number };

/** Small ZIP fixture writer; header overrides exercise hostile archives. */
export function zipFixture(entries: ZipEntry[]): Buffer {
  const local: Buffer[] = [], central: Buffer[] = [];
  let offset = 0;
  for (const entry of entries) {
    const name = Buffer.from(entry.name), data = Buffer.from(entry.data ?? "");
    const body = entry.deflate ? deflateRawSync(data) : data;
    const header = Buffer.alloc(30), directory = Buffer.alloc(46);
    header.writeUInt32LE(0x04034b50); header.writeUInt16LE(20, 4);
    header.writeUInt16LE(entry.flags ?? 0, 6); header.writeUInt16LE(entry.deflate ? 8 : 0, 8);
    header.writeUInt32LE(entry.crc ?? crc32(data), 14); header.writeUInt32LE(body.length, 18);
    header.writeUInt32LE(entry.size ?? data.length, 22); header.writeUInt16LE(name.length, 26);
    directory.writeUInt32LE(0x02014b50); directory.writeUInt16LE(0x0314, 4);
    header.copy(directory, 6, 4, 28);
    directory.writeUInt32LE(((entry.mode ?? (entry.name.endsWith("/") ? 0o040700 : 0o100600)) * 65536) >>> 0, 38);
    directory.writeUInt32LE(offset, 42);
    local.push(header, name, body); central.push(directory, name);
    offset += header.length + name.length + body.length;
  }
  const index = Buffer.concat(central), end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50); end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(index.length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, index, end]);
}

export async function packZipEntries(root: string, id = "service-health"): Promise<ZipEntry[]> {
  const entries: ZipEntry[] = [];
  const walk = async (directory: string, prefix: string) => {
    entries.push({ name: `${prefix}/` });
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const name = `${prefix}/${entry.name}`;
      if (entry.isDirectory()) await walk(join(directory, entry.name), name);
      else entries.push({ name, data: await readFile(join(directory, entry.name)) });
    }
  };
  await walk(root, id);
  for (const kind of ["actions", "policies"]) if (!entries.some(entry => entry.name === `${id}/${kind}/`)) entries.push({ name: `${id}/${kind}/` });
  return entries;
}
