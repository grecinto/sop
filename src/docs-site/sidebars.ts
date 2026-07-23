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
    'fleet-learning',
    'green-earth-tech',
    'ai-studio-memory-advantage',
    {
      type: 'category',
      label: 'SDK Documentation',
      items: ['sdk/intro', 'sdk/vector-database', 'sdk/ai-database'],
    },
    {
      type: 'category',
      label: 'Space Management',
      items: ['spaces/content-management'],
    },
  ],
};

export default sidebars;
