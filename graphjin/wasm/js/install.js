import fs from "fs-extra";

import { fileURLToPath } from "url";
import { basename, dirname, join } from "path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const src = join(__dirname, "../config");
const dst = process.env.INIT_CWD;

const newDst = join(dst, basename(src));

const opt = {
  overwrite: false,
  errorOnExist: false,
  dereference: true,
};

try {
  await fs.emptyDir(newDst);
  await fs.copy(src, newDst, opt);
} catch (err) {
  console.error(err);
  process.exit(1);
}
