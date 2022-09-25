import "./globals.js"
import pg from "pg"
import fs from "fs"
import * as _ from "./site/wasm_exec.js";

const { Client } = pg
const client = new Client({
    host: 'localhost',
    port: 5432,
    user: 'postgres',
    password: 'postgres',
    database: "42papers-development"
})

await client.connect()

const go = new Go();
const f = fs.readFileSync("serve.wasm");
const inst = await WebAssembly.instantiate(f, go.importObject);
go.run(inst.instance);

const conf = fs.readFileSync("./config/dev.yml");
await startGraphJin(conf.toString(), client, fs);