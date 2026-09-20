'use client';

import { useEffect, useId, useRef, useState } from 'react';

// Mermaid ships as a full-fat client bundle; lazy-load it so it only lands
// on pages that actually use a diagram.
type MermaidType = typeof import('mermaid').default;
let mermaidPromise: Promise<MermaidType> | null = null;
function loadMermaid(): Promise<MermaidType> {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then((mod) => {
      const m = mod.default;
      m.initialize({
        startOnLoad: false,
        theme: 'dark',
        securityLevel: 'strict',
        fontFamily: 'inherit',
      });
      return m;
    });
  }
  return mermaidPromise;
}

interface MermaidProps {
  chart: string;
}

/**
 * Renders a Mermaid diagram from its source text. Runs client-side only —
 * the Fumadocs MDX pipeline hands us the raw `\`\`\`mermaid` block content
 * from `pre > code` and we swap in the rendered SVG.
 */
export function Mermaid({ chart }: MermaidProps) {
  const id = useId().replace(/[^a-zA-Z0-9]/g, '');
  const containerRef = useRef<HTMLDivElement>(null);
  const [svg, setSvg] = useState<string>('');
  const [error, setError] = useState<string>('');

  useEffect(() => {
    let cancelled = false;
    loadMermaid()
      .then((mermaid) => mermaid.render(`mermaid-${id}`, chart))
      .then(({ svg: rendered }) => {
        if (!cancelled) setSvg(rendered);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const message = err instanceof Error ? err.message : String(err);
          setError(message);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [chart, id]);

  if (error) {
    return (
      <div className="my-4 rounded border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-500">
        Mermaid render failed: {error}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="my-4 flex justify-center overflow-x-auto rounded border border-fd-border bg-fd-card p-4"
      // The SVG comes from mermaid's own renderer; securityLevel: 'strict'
      // ensures no scripts land in it.
      dangerouslySetInnerHTML={{ __html: svg || 'Rendering…' }}
    />
  );
}
