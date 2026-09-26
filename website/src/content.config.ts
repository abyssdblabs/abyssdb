import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";

// The documentation is rendered straight from the repository's docs/ folder,
// so GitHub and the website always show the same content.
const docs = defineCollection({
  loader: glob({ pattern: "*.md", base: "../docs" }),
});

export const collections = { docs };
