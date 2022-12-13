import "./globals.js"
import fs from "fs"
import * as _ from "./runtime/wasm_exec.js";

import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const go = new Go();
const f = fs.readFileSync(join(__dirname,"./graphjin.wasm"));
const inst = await WebAssembly.instantiate(f, go.importObject);
go.run(inst.instance);

export default async function(configPath, config, db) {
    if (typeof config === 'string') {
        const conf = {value: config, isFile: true}
        return await createGraphJin(configPath, conf, db, fs)
    } else {
        const conf = {value: JSON.stringify(config), isFile: false}
        return await createGraphJin(configPath, conf, db, fs) 
    }
}