# GraphJin Website

This directory contains the Hugo-based GraphJin website. Content is written as
Markdown under `content/`, layouts live under `layouts/`, and static source
assets live under `static/`.

## Commands

```bash
npm run dev
npm run build
npm run check
```

`npm run build` syncs the root `install.sh` into `static/install.sh`, builds
the Hugo site into `public/`, and creates the Pagefind search index.
