// Minimal runtime stand-ins for the parts of the Obsidian API that Dataview's
// query engine and page parser touch outside the plugin UI.
export class Component {
    load() {}
    onload() {}
    unload() {}
    onunload() {}
    addChild<T>(c: T): T { return c; }
    removeChild<T>(c: T): T { return c; }
    register(_: () => any) {}
    registerEvent(_: any) {}
    registerInterval(n: number) { return n; }
}
export class TAbstractFile {
    constructor(public path: string, public name: string, public parent: TFolder | null) {}
}
export class TFile extends TAbstractFile {
    basename: string;
    extension: string;
    stat: { ctime: number; mtime: number; size: number } = { ctime: 0, mtime: 0, size: 0 };
    constructor(path: string, parent: TFolder | null) {
        const name = path.split("/").pop() as string;
        super(path, name, parent);
        const dot = name.lastIndexOf(".");
        this.basename = dot > 0 ? name.slice(0, dot) : name;
        this.extension = dot > 0 ? name.slice(dot + 1) : "";
    }
}
export class TFolder extends TAbstractFile {
    children: TAbstractFile[] = [];
    isRoot() { return this.path === "/"; }
}
export class Vault {}
export class MetadataCache {}
export class App {}
export class Plugin extends Component {}
export class MarkdownRenderChild extends Component {}
export class Notice { constructor(_: string) {} }
export const Platform = { isMobile: false, isDesktop: true };
export function parseYaml(_: string): any { throw new Error("parseYaml unsupported in bridge"); }
export function getAllTags(): string[] { return []; }
export function normalizePath(p: string) { return p; }
