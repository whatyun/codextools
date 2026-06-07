import { defaultLanguage } from "./languages";
import { dictionaries } from "./translations";
import type { LanguageCode, TranslationDictionary } from "./types";

const translatedAttributes = ["title", "aria-label", "aria-description", "placeholder", "alt"] as const;
const textSources = new WeakMap<Text, string>();
const attributeSources = new WeakMap<Element, Partial<Record<(typeof translatedAttributes)[number], string>>>();
let documentTitleSource = "";

const dynamicRules: Array<{
  pattern: RegExp;
  replace: (match: RegExpMatchArray, dictionary: TranslationDictionary) => string;
}> = [
  {
    pattern: /^发现 (\d+) 个 CCS Codex 供应商：(.*)$/,
    replace: (match, dictionary) => `${lookup(dictionary, "发现")} ${match[1]} ${lookup(dictionary, "个 CCS Codex 供应商")}：${match[2]}`,
  },
  {
    pattern: /^(\d+) 个市场脚本，已安装 (\d+) 个，本地整体 (关闭|开启)$/,
    replace: (match, dictionary) =>
      `${match[1]} ${lookup(dictionary, "个市场脚本")}，${lookup(dictionary, "已安装")} ${match[2]} ${lookup(dictionary, "个")}，${lookup(dictionary, "本地整体")} ${lookup(dictionary, match[3])}`,
  },
  {
    pattern: /^清单更新时间：(.*)$/,
    replace: (match, dictionary) => `${lookup(dictionary, "清单更新时间")}：${match[1]}`,
  },
  {
    pattern: /^平台：(.*)$/,
    replace: (match, dictionary) => `${lookup(dictionary, "平台")}：${match[1]}`,
  },
  {
    pattern: /^供应商 (\d+)$/,
    replace: (match, dictionary) => `${lookup(dictionary, "供应商")} ${match[1]}`,
  },
  {
    pattern: /^(.*) 副本$/,
    replace: (match, dictionary) => `${match[1]} ${lookup(dictionary, "副本")}`,
  },
];

export function translateText(value: string, language: LanguageCode): string {
  if (language === defaultLanguage) return value;
  const dictionary = dictionaries[language] ?? dictionaries[defaultLanguage];
  return translateWithDictionary(value, dictionary);
}

export function localizeDocument(root: ParentNode, language: LanguageCode) {
  const dictionary = dictionaries[language] ?? dictionaries[defaultLanguage];
  if (root instanceof Document) {
    localizeDocumentTitle(root, language, dictionary);
  }

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const textNodes: Text[] = [];
  while (walker.nextNode()) {
    textNodes.push(walker.currentNode as Text);
  }
  for (const node of textNodes) {
    localizeTextNode(node, language, dictionary);
  }

  const elements = elementsForLocalization(root);
  for (const element of elements) {
    if (shouldSkipAttributeElement(element)) continue;
    for (const attribute of translatedAttributes) {
      localizeAttribute(element, attribute, language, dictionary);
    }
  }
}

export function watchDocumentLocalization(root: ParentNode, getLanguage: () => LanguageCode): () => void {
  let frame = 0;
  const schedule = () => {
    if (frame) return;
    frame = window.requestAnimationFrame(() => {
      frame = 0;
      localizeDocument(root, getLanguage());
    });
  };

  schedule();
  if (typeof MutationObserver === "undefined") {
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
    };
  }

  const target = root instanceof Document ? root.documentElement : root;
  const observer = new MutationObserver((mutations) => {
    if (mutations.some(shouldRelocalizeMutation)) schedule();
  });
  observer.observe(target, {
    attributeFilter: [...translatedAttributes],
    attributes: true,
    characterData: true,
    childList: true,
    subtree: true,
  });

  return () => {
    if (frame) window.cancelAnimationFrame(frame);
    observer.disconnect();
  };
}

function localizeDocumentTitle(root: Document, language: LanguageCode, dictionary: TranslationDictionary) {
  const value = root.title;
  if (!value.trim()) return;
  const source =
    documentTitleSource && isRenderedFromSource(value, documentTitleSource)
      ? documentTitleSource
      : value;
  documentTitleSource = source;
  const translated = translateForLanguage(source, language, dictionary);
  if (translated !== value) root.title = translated;
}

function elementsForLocalization(root: ParentNode): Element[] {
  if (root instanceof Document) return Array.from(root.querySelectorAll("*"));
  if (root instanceof Element) return [root, ...Array.from(root.querySelectorAll("*"))];
  return Array.from(root.querySelectorAll("*"));
}

function localizeTextNode(node: Text, language: LanguageCode, dictionary: TranslationDictionary) {
  if (shouldSkipTextNode(node)) return;
  const value = node.nodeValue ?? "";
  if (!value.trim()) return value;
  const leading = value.match(/^\s*/)?.[0] ?? "";
  const trailing = value.match(/\s*$/)?.[0] ?? "";
  const inner = value.trim();
  const previousSource = textSources.get(node);
  const source =
    previousSource && isRenderedFromSource(inner, previousSource)
      ? previousSource
      : inner;
  textSources.set(node, source);

  const translated = translateForLanguage(source, language, dictionary);
  const next = `${leading}${translated}${trailing}`;
  if (next !== value) node.nodeValue = next;
}

function localizeAttribute(
  element: Element,
  attribute: (typeof translatedAttributes)[number],
  language: LanguageCode,
  dictionary: TranslationDictionary,
) {
  const value = element.getAttribute(attribute);
  if (!value?.trim()) return;
  const stored = attributeSources.get(element) ?? {};
  const previousSource = stored[attribute];
  const source =
    previousSource && isRenderedFromSource(value, previousSource)
      ? previousSource
      : value;
  attributeSources.set(element, { ...stored, [attribute]: source });
  const translated = translateForLanguage(source, language, dictionary);
  if (translated !== value) element.setAttribute(attribute, translated);
}

function translateForLanguage(value: string, language: LanguageCode, dictionary: TranslationDictionary) {
  if (language === defaultLanguage) return value;
  return translateWithDictionary(value, dictionary);
}

function translateWithDictionary(value: string, dictionary: TranslationDictionary): string {
  if (Object.prototype.hasOwnProperty.call(dictionary, value)) return dictionary[value];
  const templated = translateWithTemplates(value, dictionary);
  if (templated) return templated;
  for (const rule of dynamicRules) {
    const match = value.match(rule.pattern);
    if (match) return rule.replace(match, dictionary);
  }
  return value;
}

function translateWithTemplates(value: string, dictionary: TranslationDictionary): string {
  for (const [source, translated] of Object.entries(dictionary)) {
    if (!source.includes("{}")) continue;
    const match = value.match(templatePattern(source));
    if (!match) continue;
    return applyTemplate(translated, match.slice(1), dictionary);
  }
  return "";
}

function templatePattern(template: string) {
  const escaped = template
    .split("{}")
    .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  return new RegExp(`^${escaped.join("(.+?)")}$`);
}

function applyTemplate(template: string, values: string[], dictionary: TranslationDictionary): string {
  let index = 0;
  return template.replace(/\{(\d*)\}/g, (_, explicitIndex: string) => {
    const valueIndex = explicitIndex ? Number(explicitIndex) : index++;
    return translateWithDictionary(values[valueIndex] ?? "", dictionary);
  });
}

function lookup(dictionary: TranslationDictionary, key: string) {
  return dictionary[key] || key;
}

function isRenderedFromSource(value: string, source: string) {
  if (value === source) return true;
  return (Object.keys(dictionaries) as LanguageCode[]).some(
    (language) => translateForLanguage(source, language, dictionaries[language]) === value,
  );
}

function shouldSkipTextNode(node: Text) {
  const parent = node.parentElement;
  return !parent || shouldSkipTextElement(parent);
}

function shouldSkipTextElement(element: Element) {
  return Boolean(element.closest("script, style, code, pre, textarea, .log-lines, .log-view, [data-i18n-skip]"));
}

function shouldSkipAttributeElement(element: Element) {
  return Boolean(element.closest("script, style, code, pre, .log-lines, .log-view, [data-i18n-skip]"));
}

function shouldRelocalizeMutation(mutation: MutationRecord) {
  if (mutation.type === "characterData") {
    const parent = mutation.target.parentElement;
    return !parent || !shouldSkipTextElement(parent);
  }
  if (mutation.type === "attributes") {
    return mutation.target instanceof Element && !shouldSkipAttributeElement(mutation.target);
  }
  return Array.from(mutation.addedNodes).some((node) => {
    if (node instanceof Text) return !shouldSkipTextNode(node);
    if (node instanceof Element) return !shouldSkipTextElement(node) || !shouldSkipAttributeElement(node);
    return false;
  });
}
