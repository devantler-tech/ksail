import starlight from "@astrojs/starlight";
import mermaid from "astro-mermaid";
import { defineConfig } from "astro/config";
import starlightGithubAlerts from "starlight-github-alerts";

export default defineConfig({
  site: "https://ksail.devantler.tech",
  integrations: [
    mermaid(),
    starlight({
      title: "🛥️🐳 KSail",
      description: "Documentation for KSail - CLI tool for creating, maintaining and operating Kubernetes clusters. ☸️",
      logo: {
        dark: "./src/assets/logo-dark.png",
        light: "./src/assets/logo-light.png",
        replacesTitle: false,
      },
      favicon: "./src/assets/favicon.png",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/devantler-tech/ksail",
        },
      ],
      editLink: {
        baseUrl: "https://github.com/devantler-tech/ksail/edit/main/docs/",
      },
      customCss: ["./src/styles/custom.css"],
      plugins: [
        // Link validator disabled during development - enable in CI after CLI docs are generated
        // starlightLinksValidator(),
        starlightGithubAlerts(),
      ],
      sidebar: [
        {
          label: "Getting Started",
          items: [
            { label: "Installation", link: "/installation/" },
            { label: "Features", link: "/features/" },
          ],
        },
        {
          label: "Configuration",
          items: [
            { label: "Overview", link: "/configuration/" },
            { label: "Declarative Configuration", link: "/configuration/declarative-configuration/" },
            {
              label: "CLI Flags",
              collapsed: true,
              autogenerate: { directory: "cli-flags" },
            },
          ],
        },
        { label: "Concepts", link: "/concepts/" },
        { label: "Use Cases", link: "/use-cases/" },
        { label: "Support Matrix", link: "/support-matrix/" },
        {
          label: "Help",
          items: [
            { label: "FAQ", link: "/faq/" },
            { label: "Troubleshooting", link: "/troubleshooting/" },
          ],
        },
        { label: "Resources", link: "/resources/" },
      ],
      lastUpdated: true,
      pagination: true,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
    }),
  ],
});
