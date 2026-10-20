import type { CollectionEntry } from "astro:content";

export type Doc = CollectionEntry<"docs">;

// Sidebar order, grouped. Pages not listed here are appended to "More".
export const sections: { title: string; pages: string[] }[] = [
  { title: "Introduction", pages: ["readme", "getting-started"] },
  { title: "Reference", pages: ["sql-reference", "cli", "postgres", "mcp"] },
  { title: "Concepts", pages: ["architecture", "security"] },
  { title: "Guides", pages: ["operations", "rust", "webassembly", "deep-abyss"] },
];

export const repo = "https://github.com/noctarius/abyssdb";

export function slugOf(doc: Doc): string | undefined {
  return doc.id === "readme" ? undefined : doc.id;
}

export function titleOf(doc: Doc): string {
  return doc.rendered?.metadata?.headings?.find((h: { depth: number }) => h.depth === 1)?.text ?? doc.id;
}

export function sourceFile(doc: Doc): string {
  return doc.id === "readme" ? "README.md" : `${doc.id}.md`;
}

/** Docs in sidebar order, grouped into sections. */
export function grouped(all: Doc[]): { title: string; docs: Doc[] }[] {
  const byId = new Map(all.map((d) => [d.id, d]));
  const used = new Set<string>();
  const out = sections.map((s) => ({
    title: s.title,
    docs: s.pages.flatMap((id) => (byId.has(id) ? (used.add(id), [byId.get(id)!]) : [])),
  }));
  const rest = all.filter((d) => !used.has(d.id));
  if (rest.length) out.push({ title: "More", docs: rest });
  return out;
}

export function ordered(all: Doc[]): Doc[] {
  return grouped(all).flatMap((s) => s.docs);
}
