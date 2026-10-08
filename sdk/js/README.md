# @yachiyo-5i/xlyra-plugin-sdk

TypeScript types for xLyra JS plugins. Types only: there is no runtime code, and
`import type` is erased when the plugin is bundled.

```ts
import type { ProbeContext, ProbeDecision, ProbeStep } from "@yachiyo-5i/xlyra-plugin-sdk";
```

`xlyra-plugin init` already writes the same declarations to `types/xlyra.d.ts`, so
this package is optional. Use it if you prefer to pin the types with npm.

`index.d.ts` is generated from the Go contract structs in the xLyra server
(`go generate ./cmd/xlyra-plugin`), so it matches what the server accepts. A Go
test fails when it is stale.

License: AGPL-3.0-only. Plugins you write with it carry whatever license you choose.
