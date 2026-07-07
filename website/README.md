# tenantplane website

The source for the tenantplane documentation site, built with
[Hugo](https://gohugo.io) (extended). It uses a small custom theme that lives
entirely in this directory — no external theme or Hugo modules to install — so
the site builds fully offline.

## Structure

```
website/
├── hugo.toml            # site config, menus, params
├── content/             # Markdown: landing page + docs tree
│   ├── _index.md        # homepage (rendered by layouts/index.html)
│   └── docs/            # introduction, quickstart, concepts, guides, reference
├── layouts/             # the custom theme (templates + partials)
└── static/css/          # stylesheet (light/dark, responsive)
```

## Prerequisites

Hugo **extended**, v0.128+:

```bash
# macOS
brew install hugo
# Linux (example)
curl -sSL -o hugo.tar.gz \
  https://github.com/gohugoio/hugo/releases/download/v0.128.0/hugo_extended_0.128.0_linux-amd64.tar.gz
tar xzf hugo.tar.gz hugo && sudo mv hugo /usr/local/bin/
```

## Develop

```bash
hugo server            # live-reload dev server at http://localhost:1313
```

## Build

```bash
hugo --gc --minify     # outputs a static site to ./public
```

From the repo root you can also use `make site-serve` and `make site-build`.

## Editing docs

Docs are plain Markdown under `content/docs/`. Front matter drives navigation:

- `title` / `description` — shown in the page header and cards.
- `weight` — controls ordering in the sidebar and section listings.

The sidebar groups (Getting started / Concepts / Guides / Reference) are defined
in `layouts/partials/docs-sidebar.html`.

## Branding

Colors, tagline, and links are placeholders in `hugo.toml` (`[params]`) and
`static/css/style.css` (`:root` variables). Swap them when real branding exists.
