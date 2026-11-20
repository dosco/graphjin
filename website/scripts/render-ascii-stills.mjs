#!/usr/bin/env node

// Renders each ASCII scene's poster frame and size into data/ascii.json. Hugo
// inlines the frame so a page shows its figure before (or without) JavaScript,
// and reads the size to fit the type. Run `npm run ascii` after changing
// static/js/ascii.js; `npm run build` does it for you.
//
//   node scripts/render-ascii-stills.mjs            write data/ascii.json
//   node scripts/render-ascii-stills.mjs org 4.2    print one frame at t=4.2s

import { writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(here, '..');

await import(path.join(siteRoot, 'static', 'js', 'ascii.js'));
const { scenes, render } = globalThis.DeepOrgAscii;

const [only, at] = process.argv.slice(2);
if (only) {
  if (!scenes[only]) {
    console.error(`no scene named ${only}; have ${Object.keys(scenes).join(', ')}`);
    process.exit(1);
  }
  console.log(render(only, at === undefined ? undefined : Number(at)).toText());
  process.exit(0);
}

const stills = {};
for (const [name, scene] of Object.entries(scenes)) {
  stills[name] = { cols: scene.cols, rows: scene.rows, still: render(name).toText() };
}
await writeFile(path.join(siteRoot, 'data', 'ascii.json'), JSON.stringify(stills, null, 2) + '\n');
console.log(`Rendered ${Object.keys(stills).length} ASCII stills.`);
