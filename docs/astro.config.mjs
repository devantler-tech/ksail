import starlight from "@astrojs/starlight";
import mermaid from "astro-mermaid";
import { defineConfig } from "astro/config";
import starlightGithubAlerts from "starlight-github-alerts";

export default defineConfig({
  site: "https://ksail.devantler.tech",
  redirects: {
    "/getting-started/vanilla/": "/distributions/vanilla/",
    "/getting-started/k3s/": "/distributions/k3s/",
    "/getting-started/talos/": "/distributions/talos/",
    "/getting-started/vcluster/": "/distributions/vcluster/",
  },
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
        { label: "Concepts", link: "/concepts/" },
        { label: "Architecture", link: "/architecture/" },
        { label: "Use Cases", link: "/use-cases/" },
        { label: "Support Matrix", link: "/support-matrix/" },
        {
          label: "Guides",
          items: [
            { label: "Ephemeral Clusters (--ttl)", link: "/guides/ephemeral-clusters/" },
            { label: "Multi-Environment Workflows", link: "/guides/multi-environment/" },
            { label: "PR Preview Clusters", link: "/guides/pr-preview-clusters/" },
            { label: "Companion Tools", link: "/guides/companion-tools/" },
            { label: "KSail + mirrord", link: "/guides/mirrord/" },
            { label: "Workload Validate Substitution Expansion", link: "/guides/workload-validate/" },
          ],
        },
        {
          label: "Configuration",
          items: [
            { label: "Overview", link: "/configuration/" },
            { label: "Declarative Configuration", link: "/configuration/declarative-configuration/" },
            { label: "LoadBalancer", link: "/configuration/loadbalancer/" },
            {
              label: "CLI Flags",
              collapsed: true,
              autogenerate: { directory: "cli-flags" },
            },
          ],
        },
        {
          label: "Distributions",
          items: [
            { label: "Vanilla (Kind)", link: "/distributions/vanilla/" },
            { label: "K3s (K3d)", link: "/distributions/k3s/" },
            { label: "Talos", link: "/distributions/talos/" },
            { label: "VCluster", link: "/distributions/vcluster/" },
          ],
        },
        {
          label: "Providers",
          items: [
            { label: "Docker", link: "/providers/docker/" },
            { label: "Hetzner", link: "/providers/hetzner/" },
            { label: "Omni (Sidero)", link: "/providers/omni/" },
          ],
        },
        {
          label: "Integrations",
          items: [
            { label: "VSCode Extension", link: "/vscode-extension/" },
          ],
        },
        {
          label: "AI & Automation",
          items: [
            { label: "AI Chat Assistant", link: "/ai-chat/" },
            { label: "MCP Server", link: "/mcp/" },
            { label: "Using KSail with AI Assistants", link: "/guides/ai-mcp/" },
          ],
        },
        {
          label: "Help",
          items: [
            { label: "Development Guide", link: "/development/" },
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
