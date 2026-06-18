// Strips JSON-Schema conditional keywords (allOf/if/then/else) before running
// json-schema-to-typescript: json2ts cannot represent conditional subschemas and
// collapses any node carrying them into an index signature, losing every field.
// The conditionals (distribution x provider constraints) only restrict valid value
// combinations for editors validating ksail.yaml — they never define fields — so
// the generated TypeScript types are identical to the unconstrained shape.
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { compileFromFile } from "json-schema-to-typescript";

const CONDITIONAL_KEYWORDS = new Set(["allOf", "if", "then", "else"]);

function strip(node) {
  if (Array.isArray(node)) {
    return node.map(strip);
  }

  if (node && typeof node === "object") {
    const out = {};

    for (const [key, value] of Object.entries(node)) {
      if (CONDITIONAL_KEYWORDS.has(key)) {
        continue;
      }

      out[key] = strip(value);
    }

    return out;
  }

  return node;
}

const schema = JSON.parse(
  readFileSync(new URL("../../../schemas/ksail-config.schema.json", import.meta.url), "utf8"),
);
const tmpFile = join(mkdtempSync(join(tmpdir(), "ksail-schema-")), "ksail-config.schema.json");
writeFileSync(tmpFile, JSON.stringify(strip(schema)));

// Call json-schema-to-typescript's API directly instead of spawning `npx json2ts`.
// Spawning npx fails on Windows CI — execFileSync cannot resolve the `npx.cmd`
// shim without a shell (ENOENT), which broke the Windows desktop release job and,
// via the release-cleanup cascade, blocked publishing every new version. The CLI
// is a thin wrapper over compileFromFile, so the generated output is byte-identical
// while the cross-platform subprocess is removed entirely.
const outFile = "src/generated/ksail-config.ts";
mkdirSync(dirname(outFile), { recursive: true });
writeFileSync(outFile, await compileFromFile(tmpFile));
