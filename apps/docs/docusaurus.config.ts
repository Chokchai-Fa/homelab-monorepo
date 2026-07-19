import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// Homelab documentation site. Docs-only mode: the docs plugin is mounted at
// the site root and the blog is disabled. Mermaid is enabled site-wide for the
// architecture and sequence diagrams.
const config: Config = {
  title: 'Homelab Docs',
  tagline: 'GitOps k3s cluster, data services, and the LINE chatbot + reminder system',
  favicon: 'img/favicon.svg',

  url: 'https://docs.chokchai-dev.xyz',
  baseUrl: '/',

  // These only matter for the "edit this page" links and GitHub Pages deploys;
  // this site is served from the cluster, so they are informational.
  organizationName: 'chokchai-fa',
  projectName: 'homelab-monorepo',

  onBrokenLinks: 'throw',

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },
  themes: ['@docusaurus/theme-mermaid'],

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          // Docs-only mode: serve the docs at the site root.
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Homelab Docs',
      logo: {
        alt: 'Homelab logo',
        src: 'img/favicon.svg',
      },
      items: [
        { type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs' },
        {
          href: 'https://github.com/chokchai-fa/homelab-monorepo',
          label: 'monorepo',
          position: 'right',
        },
        {
          href: 'https://github.com/chokchai-fa/homelab-flux-controller',
          label: 'flux-controller',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Layers',
          items: [
            { label: 'Architecture', to: '/architecture/overview' },
            { label: 'Infrastructure', to: '/infrastructure/k3s-cluster' },
            { label: 'Data services', to: '/data-services/nats' },
            { label: 'Services', to: '/services/line-chatbot' },
          ],
        },
        {
          title: 'Reference',
          items: [
            { label: 'Sequence diagrams', to: '/diagrams/sequence-ai-chat' },
            { label: 'Runbooks', to: '/runbooks/reconciliation' },
          ],
        },
      ],
      copyright: `Homelab documentation — built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'sql', 'go', 'json'],
    },
    mermaid: {
      theme: { light: 'neutral', dark: 'dark' },
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
