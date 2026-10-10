import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import { pathToFileURL } from "node:url";

const require = createRequire(import.meta.url);
const mermaidRequire = createRequire(require.resolve("mermaid"));
const katex = mermaidRequire("katex");
const starlightRequire = createRequire(
  import.meta.resolve("@astrojs/starlight"),
);
const astroCodeRequire = createRequire(
  starlightRequire.resolve("astro-expressive-code"),
);
const rehypeCodeRequire = createRequire(
  astroCodeRequire.resolve("rehype-expressive-code"),
);
const { ExpressiveCode } = await import(
  pathToFileURL(rehypeCodeRequire.resolve("expressive-code"))
);

// Match Mermaid's public renderToString options in both native MathML and its
// legacy fallback. Resolve KaTeX from Mermaid so the override is tested where used.
test("Mermaid's KaTeX renders ordinary math in both output modes", () => {
  for (const output of ["mathml", "htmlAndMathml"]) {
    const rendered = katex.renderToString(String.raw`\frac{x^2}{2}+\sqrt{y}`, {
      throwOnError: true,
      displayMode: true,
      output,
    });
    assert.match(rendered, /<math/);
    assert.match(rendered, /<mfrac>/);
    assert.match(rendered, /<msup>/);
    assert.match(rendered, /<msqrt>/);
    if (output === "htmlAndMathml") {
      assert.match(rendered, /katex-html/);
    }
  }
});

test("inherited trust cannot authorize a math link", () => {
  const expression = String.raw`\href{https://example.com}{link}`;
  const inherited = Object.assign(Object.create({ trust: true }), {
    throwOnError: true,
    output: "htmlAndMathml",
  });
  assert.doesNotMatch(katex.renderToString(expression, inherited), /<a\s/);
  assert.match(
    katex.renderToString(expression, { trust: true, output: "htmlAndMathml" }),
    /<a href="https:\/\/example.com"/,
  );
});

test("Expressive Code expands nested selectors and preserves highlighted blocks", async () => {
  const code = new ExpressiveCode({
    plugins: [
      {
        name: "dependency-compatibility",
        baseStyles: `
        .compatibility {
          &:hover, &[data-state="open"] { color: red; }
          :is(.foo, .bar) > & { color: blue; }
          @media (min-width: 40rem) { & > span { color: green; } }
        }
      `,
      },
    ],
  });
  const styles = await code.getBaseStyles();
  assert.match(styles, /\.expressive-code \.compatibility:hover/);
  assert.match(
    styles,
    /\.expressive-code \.compatibility\[data-state="open"\]/,
  );
  assert.match(
    styles,
    /:is\(\.foo,\s*\.bar\) > \.expressive-code \.compatibility/,
  );
  assert.match(styles, /@media\s*\(min-width:\s*40rem\)/);
  assert.match(styles, /\.expressive-code \.compatibility > span/);

  const rendered = await code.render({
    code: "apiVersion: v1\nkind: ConfigMap",
    language: "yaml",
    meta: "{2}",
  });
  const serialized = JSON.stringify(rendered.renderedGroupAst);
  assert.match(serialized, /apiVersion/);
  assert.match(serialized, /ConfigMap/);
  assert.match(serialized, /highlight/);
});
