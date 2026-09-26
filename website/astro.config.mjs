import { defineConfig } from "astro/config";
import rehypeDocLinks from "./src/lib/rehype-doc-links.mjs";

// SITE_URL and BASE_PATH are set by the GitHub Pages workflow (from
// actions/configure-pages), so the same build works at a custom domain root
// and under https://<user>.github.io/<repo>/.
const base = process.env.BASE_PATH || "/";

export default defineConfig({
  site: process.env.SITE_URL || "https://abyssdb.xyz",
  base,
  devToolbar: { enabled: false },
  markdown: {
    shikiConfig: { theme: "github-dark-default" },
    rehypePlugins: [[rehypeDocLinks, { base }]],
  },
});
