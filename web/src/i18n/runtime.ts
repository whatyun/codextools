import { defaultLanguage } from "./languages";
import { dictionaries } from "./translations";
import type { LanguageCode, TranslationDictionary } from "./types";

const translatedAttributes = ["title", "aria-label", "placeholder"] as const;
const textSources = new WeakMap<Text, string>();
const attributeSources = new WeakMap<Element, Partial<Record<(typeof translatedAttributes)[number], string>>>();

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
  const dictionary = dictionaries[language];
  return translateWithDictionary(value, dictionary);
}

export function localizeDocument(root: ParentNode, language: LanguageCode) {
  const dictionary = dictionaries[language];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const textNodes: Text[] = [];
  while (walker.nextNode()) {
    textNodes.push(walker.currentNode as Text);
  }
  for (const node of textNodes) {
    localizeTextNode(node, language, dictionary);
  }

  if (!(root instanceof Element) && !(root instanceof Document)) return;
  const elements = root instanceof Document ? root.querySelectorAll("*") : root.querySelectorAll("*");
  for (const element of elements) {
    if (shouldSkipElement(element)) continue;
    for (const attribute of translatedAttributes) {
      localizeAttribute(element, attribute, language, dictionary);
    }
  }
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

function translateWithDictionary(value: string, dictionary: TranslationDictionary) {
  const direct = dictionary[value];
  if (direct) return direct;
  for (const rule of dynamicRules) {
    const match = value.match(rule.pattern);
    if (match) return rule.replace(match, dictionary);
  }
  return value;
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
  return !parent || shouldSkipElement(parent);
}

function shouldSkipElement(element: Element) {
  return Boolean(element.closest("script, style, code, pre, textarea, .log-lines, .log-view, [data-i18n-skip]"));
}
