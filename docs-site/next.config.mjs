import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  // Static export so GitHub Pages can serve the site without a Next runtime.
  // Trade-off: the /api/search route becomes a build-time artifact rather
  // than a live handler. Fumadocs falls back to a client-side search index
  // for static builds, so search still works from the browser.
  output: 'export',
  // GitHub Pages serves this under /shubam-ai-code-reviewer/. Setting
  // basePath tells Next to prefix every internal link and asset with it,
  // so the site works both at the root (Vercel) and under the repo path.
  basePath: process.env.NEXT_PUBLIC_BASE_PATH ?? '',
  images: {
    // Static export can't run the Next image optimizer at request time.
    unoptimized: true,
  },
  // Trailing slashes match GH Pages' directory-index convention.
  trailingSlash: true,
};

export default withMDX(config);
