// Captures ground truth from a running Obsidian for the fixture vault:
//   testdata/oracle/metadata.json  app.metadataCache.getFileCache() per note
//   testdata/oracle/results.json   dv.query() results for testdata/oracle/cases.json
// Requires: Obsidian >= 1.12 running with testdata/fixture-vault open, the
// Dataview plugin (same version as tools/dataview-bridge) enabled, and the
// `obsidian` CLI on PATH. Usage: node tools/dv-oracle/oracle.mjs [vault-name]
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";

const vault = process.argv[2] ?? "fixture-vault";
const cases = JSON.parse(readFileSync("testdata/oracle/cases.json", "utf8"));

function evalInObsidian(js) {
    const out = execFileSync("obsidian", [`vault=${vault}`, "eval", `code=${js}`], { encoding: "utf8", maxBuffer: 64 << 20 });
    const i = out.indexOf("{");
    return JSON.parse(out.slice(i).replace(/^=>\s*/, ""));
}

const serializer = `
const enc = v => {
  if (v === null || v === undefined) return null;
  if (v.isLuxonDateTime) return { $date: v.toISO() };
  if (v.isLuxonDuration) return { $dur: v.toMillis() };
  if (typeof v === "object" && "path" in v && "type" in v && "embed" in v) return { $link: v.path, subpath: v.subpath ?? null, type: v.type };
  if (Array.isArray(v) || (v.values && typeof v.array === "function")) return Array.from(v.array ? v.array() : v, enc);
  if (typeof v === "object") return { $obj: Object.entries(v).map(([k, x]) => [k, enc(x)]) };
  return v;
};`;

const meta = evalInObsidian(`(()=>{const o={};for(const f of app.vault.getMarkdownFiles()){o[f.path]=app.metadataCache.getFileCache(f)}return JSON.stringify(o)})()`);
writeFileSync("testdata/oracle/metadata.json", JSON.stringify(meta, null, 1));

const results = evalInObsidian(`(async()=>{${serializer}
const dv = app.plugins.plugins.dataview.api; const out = {};
for (const c of ${JSON.stringify(cases)}) {
  const r = await dv.query(c.query, c.origin);
  out[c.name] = r.successful ? { type: r.value.type, headers: r.value.headers ?? null, values: enc(r.value.values) } : { error: true };
}
return JSON.stringify(out)})()`);
writeFileSync("testdata/oracle/results.json", JSON.stringify(results, null, 1));
console.log(`wrote metadata for ${Object.keys(meta).length} notes and ${Object.keys(results).length} query results`);
