import * as _ from "./site/wasm_exec.js";
const go = new window.Go();
const f = await Deno.readFileSync("serve.wasm");
const inst = await WebAssembly.instantiate(f, go.importObject);
go.run(inst.instance);

console.log("Starting GraphJin: ", Deno.cwd());

try {
  const conf = await Deno.readTextFile("./config/dev.yml");
  await startGraphJin(conf);
} catch (e) {
  console.log("GraphJin Error:", e);
}
