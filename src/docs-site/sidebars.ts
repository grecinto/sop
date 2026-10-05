import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.
 */
const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'User Guide',
      items: [
        'user-guide/intro',
        'user-guide/ai-window',
        'user-guide/coding-workflows',
        'user-guide/space-management',
        'user-guide/store-management',
        'user-guide/remote-daemon-access',
      ],
    },
    'fleet-learning',
    'green-earth-tech',
    'ai-studio-memory-advantage',
    'token-efficiency-advantage',
    {
      type: 'category',
      label: 'SDK Documentation',
      items: [
        'sdk/intro',
        'sdk/history',
        'sdk/ide-copilot-backend',
        'sdk/coding-agent-integrations',
        'sdk/ask-participant-mode',
        'sdk/workflow-architecture',
        'sdk/vector-database',
        'sdk/ai-database',
        'sdk/stores-management',
        'sdk/undo-system',
      ],
    },
  ],
};

export default sidebars;
