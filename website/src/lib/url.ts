// Prefixes site-internal paths with the configured base path, so the site
// works both at a custom domain root and under a GitHub Pages project path
// (https://<user>.github.io/<repo>/).
const base = import.meta.env.BASE_URL.replace(/\/$/, "");

export function url(path: string): string {
  if (/^(https?:|mailto:|#)/.test(path)) return path;
  return base + (path.startsWith("/") ? path : `/${path}`);
}
