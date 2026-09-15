# Design lint setup

The `app` workspace owns Zen's UI lint configuration and the `@shadcn/lint`
and Oxlint development dependencies. Install dependencies from the repository
root with `bun install`; the shared lockfile is `bun.lock`.

Run from the repository root:

```bash
bun run --cwd app lint:design
```

Or run `bun run lint:design` from `app`. The script runs Oxlint against the app
using `app/.oxlintrc.json` and the existing ignore files. Android, iOS, Metro,
and build commands remain separate.

The configuration only registers the plugin through `jsPlugins`. No
`@shadcn/lint` rules, presets, overrides, or discovery settings are enabled.
Oxlint still runs its built-in default checks; existing source findings can
produce warnings or a nonzero exit status. Distinguish those findings from
configuration or plugin-loading errors. This setup verifies lint integration;
it does not enforce a design system.

Zen uses Expo/React Native components and native style tokens. This setup
does not add Tailwind or translate native styles into Tailwind policies.
Future rule choices belong in `app/.oxlintrc.json` after an explicit policy
decision. See the official [setup instructions](https://github.com/shadcn-ui/lint/blob/main/SETUP.md),
[Get started](https://github.com/shadcn-ui/lint/blob/main/README.md#get-started),
[available rules](https://github.com/shadcn-ui/lint/blob/main/README.md#rules),
and [configuration examples](https://github.com/shadcn-ui/lint/blob/main/docs/design-systems.md).

Use Bun `1.3.5` as declared by the root package. `@shadcn/lint` requires Node.js
20.19 or later and Oxlint 1.80 or later. The installed Oxlint 1.83 also requires
Node.js `^20.19.0 || >=22.12.0`; Node 24.18.0 satisfies both packages. Oxlint's
JavaScript plugin API is currently alpha.
