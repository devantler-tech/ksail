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
    "/guides/ephemeral-clusters/": "/features/ephemeral-clusters/",
    "/guides/tenant-management/": "/features/tenant-management/",
    "/guides/gateway-api/": "/configuration/gateway-api/",
    "/guides/workload-validate/": "/configuration/workload-validate/",
    "/guides/companion-tools/": "/integrations/companion-tools/",
    "/guides/mirrord/": "/integrations/mirrord/",
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
        { label: "Installation", link: "/installation/" },
        {
          label: "Features",
          items: [
            { label: "Overview", link: "/features/" },
            { label: "Cluster Provisioning", link: "/features/cluster-provisioning/" },
            { label: "Ephemeral Clusters (--ttl)", link: "/features/ephemeral-clusters/" },
            { label: "Workload Management", link: "/features/workload-management/" },
            { label: "GitOps Workflows", link: "/features/gitops-workflows/" },
            { label: "Tenant Management", link: "/features/tenant-management/" },
            { label: "Registry Management", link: "/features/registry-management/" },
            { label: "Secret Management", link: "/features/secret-management/" },
            { label: "Backup & Restore", link: "/features/backup-restore/" },
            { label: "CI/CD Integration", link: "/features/cicd-integration/" },
          ],
        },
        { label: "Concepts", link: "/concepts/" },
        { label: "Architecture", link: "/architecture/" },
        { label: "Use Cases", link: "/use-cases/" },
        {
          label: "Guides",
          items: [
            { label: "Multi-Environment Workflows", link: "/guides/multi-environment/" },
            { label: "PR Preview Clusters", link: "/guides/pr-preview-clusters/" },
            { label: "ArgoCD ApplicationSet", link: "/guides/argocd-applicationset/" },
          ],
        },
        {
          label: "Configuration",
          items: [
            { label: "Overview", link: "/configuration/" },
            { label: "Declarative Configuration", link: "/configuration/declarative-configuration/" },
            { label: "LoadBalancer", link: "/configuration/loadbalancer/" },
            { label: "Gateway API with Cilium", link: "/configuration/gateway-api/" },
            { label: "Substitution Expansion (validate)", link: "/configuration/workload-validate/" },
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
            { label: "KWOK (kwokctl)", link: "/distributions/kwok/" },
            { label: "EKS", link: "/distributions/eks/" },
          ],
        },
        {
          label: "Providers",
          items: [
            { label: "Docker", link: "/providers/docker/" },
            { label: "Hetzner", link: "/providers/hetzner/" },
            { label: "Omni (Sidero)", link: "/providers/omni/" },
            { label: "AWS", link: "/providers/aws/" },
          ],
        },
        { label: "Support Matrix", link: "/support-matrix/" },
        {
          label: "Integrations",
          items: [
            { label: "VSCode Extension", link: "/vscode-extension/" },
            { label: "Companion Tools", link: "/integrations/companion-tools/" },
            { label: "KSail + mirrord", link: "/integrations/mirrord/" },
          ],
        },
        {
          label: "AI & Automation",
          items: [
            { label: "AI Chat Assistant", link: "/ai-chat/" },
            { label: "MCP Server", link: "/mcp/" },
            { label: "AI Assistant Plugins", link: "/ai-plugins/" },
            { label: "Using KSail with AI Assistants", link: "/guides/ai-mcp/" },
          ],
        },
        {
          label: "Help & Resources",
          items: [
            { label: "FAQ", link: "/faq/" },
            { label: "Troubleshooting", link: "/troubleshooting/" },
            { label: "Development Guide", link: "/development/" },
            { label: "Benchmarks", link: "/benchmarks/" },
            { label: "Resources", link: "/resources/" },
          ],
        },
      ],
      lastUpdated: true,
      pagination: true,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
    }),
  ],
});
