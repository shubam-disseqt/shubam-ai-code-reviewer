# zreview marketing site

Single-file static landing page. No build step, no dependencies.

## Files

- `index.html` — the whole site, inline CSS
- `favicon.svg` — brand mark
- `robots.txt` — allow-all

## Deploy

Anywhere that serves static files.

### Netlify / Vercel / Cloudflare Pages

Point the deploy at this directory. No build command, publish dir = `.`.

```
Build command:   (leave blank)
Publish dir:     marketing/site
```

### Subpath of the docs site

```
cp marketing/site/index.html   /path/to/docs/public/
cp marketing/site/favicon.svg  /path/to/docs/public/
cp marketing/site/robots.txt   /path/to/docs/public/
```

### Local preview

```
python3 -m http.server -d marketing/site 8080
```

Then open http://localhost:8080.

## Editing

Everything is inline. Change copy in the `<body>`, tweak tokens at the top of
the `<style>` block (`--color-*`, `--space-*`, `--radius-*`). Keep it under
900 lines and don't add JS — the animation is CSS `@keyframes`.
