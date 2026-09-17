import { createLowlight } from "lowlight";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import css from "highlight.js/lib/languages/css";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import go from "highlight.js/lib/languages/go";
import groovy from "highlight.js/lib/languages/groovy";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import scss from "highlight.js/lib/languages/scss";
import sql from "highlight.js/lib/languages/sql";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

// Only registered languages are bundled. Adding one is an import plus two lines.
const lowlight = createLowlight({
  bash,
  c,
  cpp,
  css,
  diff,
  dockerfile,
  go,
  groovy,
  ini,
  java,
  javascript,
  json,
  markdown,
  python,
  rust,
  scss,
  sql,
  typescript,
  xml,
  yaml,
});

// A .vue file is highlighted as XML: its template is the part an excerpt usually
// shows, and highlight.js has no Vue grammar.
const languageByExtension: Record<string, string> = {
  bash: "bash", sh: "bash", zsh: "bash",
  c: "c", h: "c",
  cpp: "cpp", cc: "cpp", cxx: "cpp", hpp: "cpp",
  css: "css", scss: "scss", less: "scss",
  diff: "diff", patch: "diff",
  dockerfile: "dockerfile",
  go: "go",
  groovy: "groovy", gvy: "groovy", gy: "groovy", gsh: "groovy",
  ini: "ini", conf: "ini", toml: "ini",
  java: "java",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  json: "json",
  md: "markdown", markdown: "markdown",
  py: "python",
  rs: "rust",
  sql: "sql",
  ts: "typescript", tsx: "typescript",
  htm: "xml", html: "xml", svg: "xml", vue: "xml", xml: "xml",
  yaml: "yaml", yml: "yaml",
};

export function languageForPath(path: string): string | undefined {
  const extension = path.toLowerCase().split(".").pop() ?? "";
  const language = languageByExtension[extension];
  return language && lowlight.registered(language) ? language : undefined;
}

export type HighlightNode =
  | { type: "text"; value: string }
  | { type: "element"; className: string; children: HighlightNode[] };

type HastNode = {
  type: string;
  value?: string;
  properties?: { className?: unknown };
  children?: HastNode[];
};

function toNodes(nodes: HastNode[]): HighlightNode[] {
  const out: HighlightNode[] = [];
  for (const node of nodes) {
    if (node.type === "text") {
      out.push({ type: "text", value: node.value ?? "" });
    } else if (node.type === "element") {
      const className = Array.isArray(node.properties?.className)
        ? (node.properties.className as string[]).join(" ")
        : "";
      out.push({ type: "element", className, children: toNodes(node.children ?? []) });
    }
  }
  return out;
}

// Splitting the highlighted tree at newlines, rather than highlighting each line on
// its own, is what keeps a construct that spans lines -- a block comment, a template
// literal -- coloured as one thing instead of restarting on every line.
function splitNode(node: HighlightNode): HighlightNode[][] {
  if (node.type === "text") {
    return node.value.split("\n").map((part) => (part ? [{ type: "text" as const, value: part }] : []));
  }
  return splitChildren(node.children).map((children) =>
    children.length ? [{ ...node, children }] : [],
  );
}

function splitChildren(children: HighlightNode[]): HighlightNode[][] {
  let lines: HighlightNode[][] = [[]];
  for (const child of children) {
    const childLines = splitNode(child);
    lines[lines.length - 1] = lines[lines.length - 1].concat(childLines[0] ?? []);
    for (let index = 1; index < childLines.length; index += 1) {
      lines.push(childLines[index]);
    }
  }
  return lines;
}

// highlightLines returns one node list per line of code, so a caller laying out a
// diff can colour each row while the grammar still sees the whole excerpt.
export function highlightLines(code: string, language: string): HighlightNode[][] {
  try {
    return splitChildren(toNodes(lowlight.highlight(language, code).children as HastNode[]));
  } catch {
    // A grammar can throw on a fragment; plain text still reads fine.
    return code.split("\n").map((line) => [{ type: "text", value: line }]);
  }
}

const languageAliases: Record<string, string> = {
  shell: "bash",
  sh: "bash",
  zsh: "bash",
  golang: "go",
  py: "python",
  rb: "ruby",
  yml: "yaml",
};

// highlightCode returns a tree of highlighted nodes for an entire code snippet,
// with auto-detection fallback if no language is specified.
export function highlightCode(code: string, language?: string): HighlightNode[] {
  try {
    const normalized = language
      ? languageAliases[language.toLowerCase()] ?? language.toLowerCase()
      : undefined;
    if (normalized && lowlight.registered(normalized)) {
      return toNodes(lowlight.highlight(normalized, code).children as HastNode[]);
    }
    const auto = lowlight.highlightAuto(code);
    if (auto && (auto.data as { relevance?: number })?.relevance && (auto.data as { relevance: number }).relevance > 0) {
      return toNodes(auto.children as HastNode[]);
    }
  } catch {
    // A grammar can throw on a snippet; plain text still reads fine.
  }
  return [{ type: "text", value: code }];
}

