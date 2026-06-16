// Strips JSON-Schema conditional keywords (allOf/if/then/else) before running
// json-schema-to-typescript: json2ts cannot represent conditional subschemas and
// collapses any node carrying them into an index signature, losing every field.
// The conditionals (distribution x provider constraints) only restrict valid value
// combinations for editors validating ksail.yaml — they never define fields — so
// the generated TypeScript types are identical to the unconstrained shape.
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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
execFileSync("npx", ["json2ts", "-i", tmpFile, "-o", "src/generated/ksail-config.ts"], {
  stdio: "inherit",
});
