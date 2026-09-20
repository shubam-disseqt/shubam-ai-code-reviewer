import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { appName, gitConfig } from './shared';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <span className="inline-flex items-center gap-2 font-semibold tracking-tight">
          <span
            aria-hidden
            className="inline-block h-2 w-2 rounded-full bg-emerald-500 shadow-[0_0_10px_rgba(16,185,129,0.7)]"
          />
          {appName}
        </span>
      ),
    },
    links: [
      { text: 'Docs', url: '/docs', active: 'nested-url' },
      { text: 'Quickstart', url: '/docs/getting-started/quickstart' },
      { text: 'Architecture', url: '/docs/architecture' },
      { text: 'CLI', url: '/docs/reference/cli' },
    ],
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
