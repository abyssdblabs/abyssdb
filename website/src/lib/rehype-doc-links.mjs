// Rewrites links inside the rendered docs:
//   sql-reference.md#select  ->  <base>/docs/sql-reference/#select
//   README.md                ->  <base>/docs/
//   ../rust/abyss-core       ->  https://github.com/abyssdblabs/abyssdb/tree/main/rust/abyss-core
const repo = "https://github.com/abyssdblabs/abyssdb";

export default function rehypeDocLinks({ base = "/" } = {}) {
  const prefix = base.replace(/\/$/, "");
  const rewrite = (href) => {
    if (/^(https?:|mailto:|#|\/)/.test(href)) return href;
    const doc = href.match(/^(?:\.\/)?([\w-]+)\.md(#.*)?$/);
    if (doc) {
      const [, name, hash = ""] = doc;
      return name.toLowerCase() === "readme" ? `${prefix}/docs/${hash}` : `${prefix}/docs/${name}/${hash}`;
    }
    if (href.startsWith("../")) {
      const path = href.replace(/^(\.\.\/)+/, "");
      const isFile = /\.[a-z0-9]+(#.*)?$/i.test(path);
      return `${repo}/${isFile ? "blob" : "tree"}/main/${path}`;
    }
    return href;
  };
  const walk = (node) => {
    if (node.type === "element" && node.tagName === "a" && typeof node.properties?.href === "string") {
      node.properties.href = rewrite(node.properties.href);
      if (/^https?:/.test(node.properties.href)) node.properties.rel = ["noopener"];
    }
    if (node.children) node.children = node.children.map(walk);
    if (node.type === "element" && node.tagName === "table") {
      // Wrap tables so wide ones scroll horizontally instead of breaking the layout.
      return { type: "element", tagName: "div", properties: { className: ["table-wrap"] }, children: [node] };
    }
    return node;
  };
  return (tree) => walk(tree);
}
