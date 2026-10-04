# web

The browser app: Vite, TypeScript, React. No router; it is an application with
panes and settings, not a site with pages.

## Dependencies

Install in a container, never on the host:

    docker run --rm -u "$(id -u):$(id -g)" --memory=512m --memory-swap=512m --cpus=2 \
      -v "$PWD":/src -w /src/web node:24.10 npm ci

## Build

`npm run build` typechecks and writes the bundle to `../internal/ui/static/dist`,
where the Go binary embeds it. In development the Go server proxies to the Vite
dev server instead, so nothing is built.

## Styling

CSS modules, one per component (`X.module.css` beside `X.tsx`). Patterns used
by several components live in `src/shared/`. `src/global.css` holds only the
theme root and the universal rules, such as hover highlighting.

Every module wraps its rules in `@layer components`. The global rules are
unlayered, so they win over any component rule regardless of selector weight.
Keep that: a universal behaviour must never need a component to cooperate.
