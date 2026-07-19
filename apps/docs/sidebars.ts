import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';

// Explicit sidebar so the ordering below is the reading order, independent of
// filenames. Each id is the doc path relative to docs/ without the extension.
const sidebars: SidebarsConfig = {
  docs: [
    'intro',
    {
      type: 'category',
      label: 'Architecture',
      collapsed: false,
      items: ['architecture/overview', 'architecture/repo-layout'],
    },
    {
      type: 'category',
      label: 'Infrastructure',
      items: [
        'infrastructure/k3s-cluster',
        'infrastructure/gitops-fluxcd',
        'infrastructure/cicd-pipeline',
        'infrastructure/networking',
        'infrastructure/monitoring',
        'infrastructure/secrets-bootstrap',
      ],
    },
    {
      type: 'category',
      label: 'Data services',
      items: ['data-services/nats', 'data-services/postgres', 'data-services/redis'],
    },
    {
      type: 'category',
      label: 'Services',
      items: [
        'services/line-chatbot',
        'services/reminder-system',
        'services/service-reference',
      ],
    },
    {
      type: 'category',
      label: 'Sequence diagrams',
      items: [
        'diagrams/sequence-ai-chat',
        'diagrams/sequence-reminder-create',
        'diagrams/sequence-reminder-fire',
      ],
    },
    {
      type: 'category',
      label: 'Runbooks',
      items: [
        'runbooks/reconciliation',
        'runbooks/push-quota-429',
        'runbooks/redis-restart',
        'runbooks/rollout-waves',
      ],
    },
  ],
};

export default sidebars;
