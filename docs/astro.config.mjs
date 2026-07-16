import starlight from "@astrojs/starlight";
import mermaid from "astro-mermaid";
import { defineConfig } from "astro/config";
import starlightGithubAlerts from "starlight-github-alerts";
import starlightLinksValidator from "starlight-links-validator";

export default defineConfig({
  site: "https://ksail.devantler.tech",
  // GFM (tables, strikethrough, autolinks) is on by default, but on Astro 6.4.x
  // the implicit default is lost when integrations use the deprecated
  // markdown.remarkPlugins hook — leaving every table rendered as literal pipe
  // text. Pin it explicitly so the site never silently loses table rendering.
  markdown: { gfm: true },
  redirects: {
    // Legacy getting-started → distributions
    "/getting-started/vanilla/": "/distributions/vanilla/",
    "/getting-started/k3s/": "/distributions/k3s/",
    "/getting-started/talos/": "/distributions/talos/",
    "/getting-started/vcluster/": "/distributions/vcluster/",
    // Features → Guides (reframed as task-oriented how-to guides)
    "/features/": "/guides/",
    "/features/cluster-provisioning/": "/guides/cluster-provisioning/",
    "/features/day-2-operations/": "/guides/day-2-operations/",
    "/features/ephemeral-clusters/": "/guides/ephemeral-clusters/",
    "/features/workload-management/": "/guides/workload-management/",
    "/features/gitops-workflows/": "/guides/gitops-workflows/",
    "/features/tenant-management/": "/guides/tenant-management/",
    "/features/registry-management/": "/guides/registry-management/",
    "/features/secret-management/": "/guides/secret-management/",
    "/features/backup-restore/": "/guides/backup-restore/",
    "/features/cicd-integration/": "/guides/cicd-integration/",
    "/features/web-ui/": "/guides/web-ui/",
    "/features/operator/": "/guides/operator/",
    // Architecture folded under Concepts; Use Cases folded into Quickstart + Guides
    "/architecture/": "/concepts/architecture/",
    "/use-cases/": "/guides/",
    // Standalone interface/AI pages → Integrations
    "/ai-chat/": "/integrations/ai-chat/",
    "/ai-plugins/": "/integrations/ai-plugins/",
    "/mcp/": "/integrations/mcp/",
    "/vscode-extension/": "/integrations/vscode-extension/",
    "/guides/ai-mcp/": "/integrations/ai-mcp/",
    // Other legacy guide aliases (targets unchanged)
    "/guides/gateway-api/": "/configuration/gateway-api/",
    "/guides/workload-validate/": "/configuration/workload-validate/",
    "/guides/companion-tools/": "/integrations/companion-tools/",
    "/guides/mirrord/": "/integrations/mirrord/",
  },
  integrations: [
    mermaid(),
    starlight({
      title: "KSail",
      description: "Documentation for KSail - CLI tool for creating, maintaining and operating Kubernetes clusters. ☸️",
      head: [
        // PNG favicon fallback for browsers that don't support SVG favicons.
        {
          tag: "link",
          attrs: { rel: "icon", type: "image/png", href: "/favicon.png", sizes: "512x512" },
        },
        {
          tag: "link",
          attrs: { rel: "preconnect", href: "https://fonts.googleapis.com" },
        },
        {
          tag: "link",
          attrs: { rel: "preconnect", href: "https://fonts.gstatic.com", crossorigin: true },
        },
        {
          tag: "link",
          attrs: {
            rel: "stylesheet",
            href: "https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,500..800&family=Hanken+Grotesque:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap",
          },
        },
        // Umami privacy-first web analytics (self-hosted on the platform).
        {
          tag: "script",
          attrs: {
            src: "https://analytics.platform.devantler.tech/script.js",
            "data-website-id": "27f83d6b-8ce4-4239-9387-eabc7f57cd68",
            // Client-side host allowlist: the tracker only sends events when
            // the page is served from this host, keeping forks, previews and
            // local builds out of analytics (not a server-side spoof guard).
            "data-domains": "ksail.devantler.tech",
            defer: true,
          },
        },
      ],
      logo: {
        src: "./src/assets/logo.svg",
        replacesTitle: false,
      },
      favicon: "/favicon.svg",
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
        starlightLinksValidator(),
        starlightGithubAlerts(),
      ],
      sidebar: [
        {
          label: "Start Here",
          items: [
            { label: "Installation", link: "/installation/" },
            { label: "Quickstart", link: "/start/quickstart/" },
            { label: "Deliver with GitOps", link: "/start/deliver-with-gitops/" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "Overview", link: "/guides/" },
            {
              label: "Cluster lifecycle",
              items: [
                { label: "Cluster Provisioning", link: "/guides/cluster-provisioning/" },
                { label: "Day-2 Operations", link: "/guides/day-2-operations/" },
                { label: "Ephemeral Clusters (--ttl)", link: "/guides/ephemeral-clusters/" },
                { label: "Backup & Restore", link: "/guides/backup-restore/" },
                { label: "Unmanaged Clusters", link: "/guides/unmanaged-clusters/" },
                { label: "Multi-Environment Workflows", link: "/guides/multi-environment/" },
              ],
            },
            {
              label: "Workloads & GitOps",
              items: [
                { label: "Workload Management", link: "/guides/workload-management/" },
                { label: "Local Service Intercepts", link: "/guides/local-service-intercepts/" },
                { label: "GitOps Workflows", link: "/guides/gitops-workflows/" },
                { label: "Registry Management", link: "/guides/registry-management/" },
                { label: "Secret Management", link: "/guides/secret-management/" },
              ],
            },
            {
              label: "Platform & teams",
              items: [
                { label: "Tenant Management", link: "/guides/tenant-management/" },
                { label: "OIDC Authentication", link: "/guides/oidc-authentication/" },
                { label: "CI/CD Integration", link: "/guides/cicd-integration/" },
                { label: "PR Preview Clusters", link: "/guides/pr-preview-clusters/" },
                { label: "ArgoCD ApplicationSet", link: "/guides/argocd-applicationset/" },
              ],
            },
            {
              label: "Interfaces",
              items: [
                { label: "Web UI & Desktop App", link: "/guides/web-ui/", badge: { text: "New", variant: "tip" } },
                { label: "Kubernetes Operator", link: "/guides/operator/", badge: { text: "New", variant: "tip" } },
              ],
            },
            {
              label: "Talos-specific",
              items: [
                { label: "Talos Disk Encryption", link: "/guides/talos-disk-encryption/" },
                { label: "Talos Native Patches", link: "/guides/talos-native-patches/" },
              ],
            },
          ],
        },
        {
          label: "Distributions & Providers",
          items: [
            { label: "Choose your setup", link: "/distributions/" },
            {
              label: "Distributions",
              items: [
                { label: "Vanilla (Kind)", link: "/distributions/vanilla/" },
                { label: "K3s (K3d)", link: "/distributions/k3s/" },
                { label: "Talos", link: "/distributions/talos/" },
                { label: "VCluster", link: "/distributions/vcluster/" },
                { label: "KWOK (kwokctl)", link: "/distributions/kwok/" },
                { label: "EKS", link: "/distributions/eks/" },
                { label: "GKE", link: "/distributions/gke/" },
                { label: "AKS", link: "/distributions/aks/" },
              ],
            },
            {
              label: "Providers",
              items: [
                { label: "Docker", link: "/providers/docker/" },
                { label: "Kubernetes (Nested)", link: "/providers/kubernetes/" },
                { label: "Hetzner", link: "/providers/hetzner/" },
                { label: "Omni (Sidero)", link: "/providers/omni/" },
                { label: "AWS", link: "/providers/aws/" },
                { label: "GCP", link: "/providers/gcp/" },
                { label: "Azure", link: "/providers/azure/" },
              ],
            },
            { label: "Support Matrix", link: "/support-matrix/" },
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
              items: [{ autogenerate: { directory: "cli-flags", collapsed: true } }],
            },
          ],
        },
        {
          label: "Concepts",
          items: [
            { label: "Core Concepts", link: "/concepts/" },
            { label: "Architecture", link: "/concepts/architecture/" },
            { label: "Project Structure & GitOps Layout", link: "/concepts/project-structure/" },
            { label: "Reference Architecture", link: "/concepts/reference-architecture/" },
          ],
        },
        {
          label: "Integrations",
          items: [
            { label: "VSCode Extension", link: "/integrations/vscode-extension/" },
            { label: "AI Chat Assistant", link: "/integrations/ai-chat/" },
            { label: "MCP Server", link: "/integrations/mcp/" },
            { label: "AI Assistant Plugins", link: "/integrations/ai-plugins/" },
            { label: "Using KSail with AI Assistants", link: "/integrations/ai-mcp/" },
            { label: "Companion Tools", link: "/integrations/companion-tools/" },
            { label: "KSail + mirrord", link: "/integrations/mirrord/" },
          ],
        },
        {
          label: "Help & Resources",
          items: [
            { label: "FAQ", link: "/faq/" },
            { label: "Troubleshooting", link: "/troubleshooting/" },
            { label: "Development Guide", link: "/development/" },
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
