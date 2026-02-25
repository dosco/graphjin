import { chmodSync, copyFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..', '..');
const source = resolve(repoRoot, 'install.sh');
const destination = resolve(repoRoot, 'website', 'public', 'install.sh');

copyFileSync(source, destination);
chmodSync(destination, 0o755);
console.log(`Synced install script to ${destination}`);
