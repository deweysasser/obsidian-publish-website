// Builds the Pagefind index for a site through Pagefind's Node API, adding
// pages in the order listed in orderFile (one site-relative .html path per
// line). Pagefind numbers pages in the order they arrive, so a stable order
// keeps an incremental deploy from renaming the whole index. Pages are added
// one at a time on purpose: concurrent adds would number them in completion
// order.
//
// Usage: node indexer.mjs <siteDir> <orderFile>
// Exits 3, printing PAGEFIND_NODE_API_UNAVAILABLE, when the pagefind npm
// package can't be loaded, so the caller can fall back to the CLI.
import { readFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";
import path from "node:path";

const [siteDir, orderFile] = process.argv.slice(2);

let pagefind;
try {
  const root = execFileSync("npm", ["root", "-g"], { encoding: "utf8" }).trim();
  pagefind = await import(pathToFileURL(path.join(root, "pagefind", "lib", "index.js")).href);
} catch (e) {
  console.log(`PAGEFIND_NODE_API_UNAVAILABLE: ${e.message}`);
  process.exit(3);
}

function check(errors, what) {
  if (errors && errors.length) throw new Error(`${what}: ${errors.join("; ")}`);
}

const order = (await readFile(orderFile, "utf8")).split("\n").filter(Boolean);
process.chdir(siteDir); // sourcePath must be relative to the working directory
const { index, errors } = await pagefind.createIndex({});
check(errors, "createIndex");
for (const rel of order) {
  const content = await readFile(rel, "utf8");
  const r = await index.addHTMLFile({ sourcePath: rel, content });
  check(r.errors, rel);
}
const w = await index.writeFiles({ outputPath: "pagefind" });
check(w.errors, "writeFiles");
console.log(`indexed ${order.length} pages in order`);
// Not the API's close(): it SIGTERMs the pagefind --service child, and where
// a security policy refuses that signal (seen as kill EACCES in a rootful
// podman container with all capabilities dropped) its error handler retries
// close() forever.
// Exiting closes the child's stdin, and the service exits on EOF by itself.
process.exit(0);
