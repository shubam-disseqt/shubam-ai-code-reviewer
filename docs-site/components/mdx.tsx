import defaultMdxComponents from 'fumadocs-ui/mdx';
import type { MDXComponents } from 'mdx/types';
import { Mermaid } from './mermaid';

// Intercept fenced code blocks. Fumadocs renders `<pre><code>` from a
// ```lang fence; when lang === 'mermaid' we swap in the client component
// that runs mermaid.js and produces an SVG. Everything else falls through
// to the default Shiki-highlighted <pre>.
function Pre(props: React.HTMLAttributes<HTMLPreElement>) {
  const child = extractOnlyCodeChild(props.children);
  if (child && codeLanguage(child) === 'mermaid') {
    const source = codeText(child);
    return <Mermaid chart={source} />;
  }
  const DefaultPre = defaultMdxComponents.pre;
  return DefaultPre ? <DefaultPre {...props} /> : <pre {...props} />;
}

function extractOnlyCodeChild(children: React.ReactNode): React.ReactElement | null {
  if (!children) return null;
  if (Array.isArray(children)) {
    const filtered = children.filter((c) => c !== '\n' && c !== '');
    if (filtered.length === 1 && isReactElement(filtered[0])) return filtered[0];
    return null;
  }
  return isReactElement(children) ? children : null;
}

function isReactElement(node: unknown): node is React.ReactElement {
  return (
    typeof node === 'object' &&
    node !== null &&
    'type' in (node as object) &&
    'props' in (node as object)
  );
}

function codeLanguage(el: React.ReactElement): string | null {
  const props = el.props as { className?: string } | undefined;
  const className = props?.className ?? '';
  const match = className.match(/language-([^\s]+)/);
  return match ? match[1] : null;
}

function codeText(el: React.ReactElement): string {
  const props = el.props as { children?: React.ReactNode } | undefined;
  return childrenToString(props?.children);
}

function childrenToString(children: React.ReactNode): string {
  if (children == null || children === false || children === true) return '';
  if (typeof children === 'string') return children;
  if (typeof children === 'number') return String(children);
  if (Array.isArray(children)) return children.map(childrenToString).join('');
  if (isReactElement(children)) {
    const props = children.props as { children?: React.ReactNode } | undefined;
    return childrenToString(props?.children);
  }
  return '';
}

export function getMDXComponents(components?: MDXComponents) {
  return {
    ...defaultMdxComponents,
    pre: Pre,
    Mermaid,
    ...components,
  } satisfies MDXComponents;
}

export const useMDXComponents = getMDXComponents;

declare global {
  type MDXProvidedComponents = ReturnType<typeof getMDXComponents>;
}
