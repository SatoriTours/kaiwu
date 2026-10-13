import { Marked, Renderer } from "marked";
import DOMPurify from "dompurify";
import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import css from "highlight.js/lib/languages/css";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import nim from "highlight.js/lib/languages/nim";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

for (const [name, language] of Object.entries({
  bash,
  css,
  go,
  javascript,
  json,
  kotlin,
  nim,
  python,
  rust,
  sql,
  typescript,
  xml,
  yaml,
}))
  hljs.registerLanguage(name, language);

export type ContentFormat = "markdown" | "json" | "text";

function escape(text: string): string {
  return text.replace(
    /[&<>"']/g,
    (c) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[c]!,
  );
}

function codeBlock(text: string, info = ""): string {
  const language = info.trim().split(/\s+/)[0]?.toLowerCase() || "text";
  let content = escape(text);
  // Large/unknown blocks remain complete plain text; do not guess a language on each stream update.
  if (text.length <= 20_000 && hljs.getLanguage(language)) {
    try {
      content = hljs.highlight(text, { language, ignoreIllegals: true }).value;
    } catch {
      /* plain text */
    }
  }
  return `<div class="code-block"><div class="code-toolbar"><span>${escape(language)}</span><button type="button" class="copy-code" data-copy-code="" aria-label="复制代码" title="复制代码">复制</button></div><pre tabindex="0"><code class="hljs">${content}</code></pre></div>`;
}

function safeLink(href: string): boolean {
  return /^(?:https?:\/\/|mailto:)/i.test(href);
}

const markdown = new Marked({
  gfm: true,
  breaks: true,
  async: false,
  renderer: {
    html({ text }) {
      return escape(text);
    },
    code({ text, lang }) {
      return codeBlock(text, lang);
    },
    table(token) {
      return `<div class="markdown-table" tabindex="0">${Renderer.prototype.table.call(this, token)}</div>`;
    },
    link({ href, title, tokens }) {
      const label = this.parser.parseInline(tokens);
      return safeLink(href)
        ? `<a href="${escape(href)}" title="${escape(title || "")}" target="_blank" rel="noopener noreferrer">${label}</a>`
        : label;
    },
    // Keep image descriptions as links rather than loading URLs embedded in Agent output.
    image({ href, text }) {
      const label = escape(text || "图片");
      return safeLink(href)
        ? `<a href="${escape(href)}" target="_blank" rel="noopener noreferrer">${label}</a>`
        : label;
    },
  },
});

export function renderContent(
  source: string,
  format: ContentFormat = "markdown",
): string {
  let html: string;
  if (format === "json") {
    let text = source;
    try {
      text = JSON.stringify(JSON.parse(source), null, 2);
    } catch {
      /* partial result/log */
    }
    html = codeBlock(text, "json");
  } else if (format === "text" || source.length > 100_000) {
    html = codeBlock(source);
  } else {
    try {
      html = markdown.parse(source, { async: false });
    } catch {
      html = codeBlock(source);
    }
  }
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: [
      "p",
      "br",
      "h1",
      "h2",
      "h3",
      "h4",
      "h5",
      "h6",
      "blockquote",
      "ul",
      "ol",
      "li",
      "strong",
      "em",
      "del",
      "a",
      "code",
      "pre",
      "div",
      "span",
      "table",
      "thead",
      "tbody",
      "tr",
      "th",
      "td",
      "hr",
      "input",
      "button",
    ],
    ALLOWED_ATTR: [
      "href",
      "title",
      "target",
      "rel",
      "class",
      "type",
      "disabled",
      "checked",
      "start",
      "align",
      "tabindex",
      "data-copy-code",
      "aria-label",
    ],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
  });
}
