// obsidian-publish dataview bridge: runs Dataview's own page parser and query
// engine over metadata supplied by the Go program. Protocol: one JSON request
// on stdin, one JSON response on stdout.
import { parsePage } from "data-import/markdown-file";
import { PageMetadata } from "data-model/markdown";
import { IndexMap, ValueCaseInsensitiveIndexMap, PrefixIndex } from "data-index/index";
import { parseQuery } from "query/parse";
import { executeList, executeTable, executeTask, executeInline } from "query/engine";
import { EXPRESSION } from "expression/parse";
import { DEFAULT_QUERY_SETTINGS, QuerySettings } from "settings";
import { Link, Values, Widgets } from "data-model/value";
import { DateTime, Duration } from "luxon";
import { TFile, TFolder } from "obsidian";
import * as fs from "fs";

interface InFile { path: string; content: string; ctime: number; mtime: number; size: number; metadata: any }
interface Request {
    settings?: Partial<QuerySettings>;
    files: InFile[];
    attachments: string[];
    aliases: Record<string, string>;
    requests: { id: string; kind: "query" | "inline"; origin: string; source: string }[];
}

function buildVault(paths: string[]) {
    const root = new TFolder("/", "", null);
    const byPath = new Map<string, any>([["/", root], ["", root]]);
    const folder = (p: string): TFolder => {
        if (p === "" || p === "/") return root;
        let f = byPath.get(p);
        if (f) return f;
        const parent = folder(p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");
        f = new TFolder(p, p.split("/").pop() as string, parent);
        parent.children.push(f);
        byPath.set(p, f);
        return f;
    };
    for (const p of paths) {
        const parent = folder(p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");
        const f = new TFile(p, parent);
        parent.children.push(f);
        byPath.set(p, f);
    }
    return { getAbstractFileByPath: (p: string) => byPath.get(p) ?? null, byPath };
}

// Obsidian link resolution; mirrors internal/index Resolve in Go.
function makeResolver(paths: string[], aliases: Record<string, string>) {
    const lower = new Map<string, string>();
    const byName = new Map<string, string[]>();
    for (const p of paths) {
        lower.set(p.toLowerCase(), p);
        let base = p.split("/").pop()!.toLowerCase();
        if (base.endsWith(".md")) base = base.slice(0, -3);
        byName.set(base, [...(byName.get(base) ?? []), p]);
    }
    const dirOf = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : ".");
    const pick = (cands: string[], from: string) => {
        if (!cands.length) return undefined;
        const d = dirOf(from);
        const same = cands.find(c => dirOf(c) === d);
        if (same) return same;
        return [...cands].sort((a, b) => {
            const da = a.split("/").length, db = b.split("/").length;
            return da - db || a.length - b.length || (a < b ? -1 : a > b ? 1 : 0);
        })[0];
    };
    const clean = (p: string) => {
        const out: string[] = [];
        for (const s of p.split("/")) {
            if (s === "..") out.pop();
            else if (s !== "." && s !== "") out.push(s);
        }
        return out.join("/");
    };
    return (target: string, from: string): string | undefined => {
        target = target.trim().replace(/^\//, "");
        if (!target) return undefined;
        const lt = target.toLowerCase();
        const forms = lt.endsWith(".md") ? [lt] : /\.[a-z0-9]{1,5}$/i.test(lt) ? [lt, lt + ".md"] : [lt + ".md"];
        const d = dirOf(from).toLowerCase();
        for (const f of forms) {
            if (lower.has(f)) return lower.get(f);
            if (d !== "." && lower.has(clean(d + "/" + f))) return lower.get(clean(d + "/" + f));
        }
        for (const f of forms) {
            let base = f.split("/").pop()!;
            if (base.endsWith(".md")) base = base.slice(0, -3);
            let cands = byName.get(base) ?? [];
            if (f.includes("/")) cands = cands.filter(c => c.toLowerCase().endsWith("/" + clean(f).replace(/^(\.\.\/)+/, "")));
            const p = pick(cands, from);
            if (p) return p;
        }
        return aliases[lt];
    };
}

function encode(v: any, settings: QuerySettings): any {
    const w = Values.wrapValue(v);
    if (!w) return null;
    switch (w.type) {
        case "null": return null;
        case "string": case "number": case "boolean": return w.value;
        case "date": return { $date: (w.value as DateTime).toISO(), text: Values.toString(w.value, settings) };
        case "duration": return { $dur: (w.value as Duration).toMillis(), text: Values.toString(w.value, settings) };
        case "link": {
            const l = w.value as Link;
            return { $link: l.path, subpath: l.subpath ?? null, type: l.type, display: l.display ?? null, embed: l.embed };
        }
        case "array": return w.value.map((x: any) => encode(x, settings));
        case "object": return { $obj: Object.entries(w.value).map(([k, x]) => [k, encode(x, settings)]) };
        case "function": return { $function: true };
        case "widget": {
            const wd: any = w.value;
            if (Widgets.isListPair(wd)) return { $pair: [encode(wd.key, settings), encode(wd.value, settings)] };
            if (Widgets.isExternalLink(wd)) return { $elink: wd.url, display: wd.display ?? null };
            return { $widget: wd.markdown() };
        }
        case "html": return { $text: String(w.value) };
    }
    return { $text: String(v) };
}

function encodeTask(t: any, settings: QuerySettings): any {
    return {
        text: t.text, status: t.status ?? null, task: !!t.task, checked: !!t.checked, completed: !!t.completed,
        line: t.line, path: t.path, visual: t.visual ?? null,
        children: (t.children ?? []).map((c: any) => encodeTask(c, settings)),
    };
}

function encodeGrouping(g: any, settings: QuerySettings): any {
    if (Array.isArray(g)) {
        return g.map((x: any) => (x && typeof x === "object" && "key" in x && "rows" in x)
            ? { key: encode(x.key, settings), rows: encodeGrouping(x.rows, settings) }
            : encodeTask(x, settings));
    }
    return g;
}

async function main() {
    const req: Request = JSON.parse(fs.readFileSync(0, "utf8"));
    const settings: QuerySettings = { ...DEFAULT_QUERY_SETTINGS, ...(req.settings ?? {}) };
    const allPaths = [...req.files.map(f => f.path), ...req.attachments];
    const vault = buildVault(allPaths);
    const resolve = makeResolver(allPaths, req.aliases ?? {});
    const resolvedLinks: Record<string, Record<string, number>> = {};
    const metadataCache = {
        getFirstLinkpathDest: (link: string, from: string) => {
            const p = resolve(link, from);
            return p ? vault.byPath.get(p) : null;
        },
        resolvedLinks,
    };
    const index: any = {
        pages: new Map<string, PageMetadata>(),
        tags: new ValueCaseInsensitiveIndexMap(),
        etags: new ValueCaseInsensitiveIndexMap(),
        links: new IndexMap(),
        prefix: new PrefixIndex(vault as any, () => {}),
        starred: { starred: () => false },
        csv: { get: async () => ({ successful: false, error: "CSV sources are not supported" }) },
        metadataCache,
        vault: { getMarkdownFiles: () => req.files.map(f => vault.byPath.get(f.path)) },
    };
    for (const f of req.files) {
        const parsed = parsePage(f.path, f.content, { ctime: f.ctime, mtime: f.mtime, size: f.size }, f.metadata);
        const meta = PageMetadata.canonicalize(parsed, link => {
            const real = resolve(link.path, f.path);
            return real ? link.withPath(real) : link;
        });
        index.pages.set(f.path, meta);
        index.tags.set(f.path, meta.fullTags());
        index.etags.set(f.path, meta.tags);
        index.links.set(f.path, new Set<string>(meta.links.map((l: Link) => l.path)));
        const rl: Record<string, number> = {};
        for (const l of meta.links) rl[l.path] = (rl[l.path] ?? 0) + 1;
        resolvedLinks[f.path] = rl;
    }

    const out: any[] = [];
    for (const r of req.requests) {
        try {
            if (r.kind === "inline") {
                const field = EXPRESSION.field.tryParse(r.source);
                const res = executeInline(field, r.origin, index, settings);
                out.push(res.successful ? { id: r.id, ok: true, value: encode(res.value, settings) } : { id: r.id, ok: false, error: res.error });
                continue;
            }
            const q = parseQuery(r.source);
            if (!q.successful) { out.push({ id: r.id, ok: false, error: q.error }); continue; }
            const query = q.value;
            switch (query.header.type) {
                case "table": {
                    const res = await executeTable(query, index, r.origin, settings);
                    if (!res.successful) { out.push({ id: r.id, ok: false, error: res.error }); break; }
                    out.push({ id: r.id, ok: true, type: "table", headers: res.value.names, idColumn: query.header.showId,
                        grouped: res.value.idMeaning.type === "group",
                        rows: res.value.data.map(row => row.map(v => encode(v, settings))) });
                    break;
                }
                case "list": {
                    const res = await executeList(query, index, r.origin, settings);
                    if (!res.successful) { out.push({ id: r.id, ok: false, error: res.error }); break; }
                    out.push({ id: r.id, ok: true, type: "list", grouped: res.value.primaryMeaning.type === "group",
                        items: res.value.data.map(v => encode(v, settings)) });
                    break;
                }
                case "task": {
                    const res = await executeTask(query, r.origin, index, settings);
                    if (!res.successful) { out.push({ id: r.id, ok: false, error: res.error }); break; }
                    out.push({ id: r.id, ok: true, type: "task", tasks: encodeGrouping(res.value.tasks, settings) });
                    break;
                }
                default:
                    out.push({ id: r.id, ok: false, unsupported: true, error: `${query.header.type} queries are not supported` });
            }
        } catch (e: any) {
            out.push({ id: r.id, ok: false, error: String(e?.message ?? e) });
        }
    }
    process.stdout.write(JSON.stringify({ results: out }));
}

main().catch(e => { process.stderr.write(String(e?.stack ?? e)); process.exit(1); });
