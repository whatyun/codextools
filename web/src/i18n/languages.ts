import type { LanguageCode, LanguageOption } from "./types";

export const defaultLanguage: LanguageCode = "zh-CN";

export const languageOptions: LanguageOption[] = [
  { code: "zh-CN", nativeName: "简体中文", englishName: "Chinese" },
  { code: "en-US", nativeName: "English", englishName: "English" },
  { code: "ko-KR", nativeName: "한국어", englishName: "Korean" },
  { code: "ja-JP", nativeName: "日本語", englishName: "Japanese" },
];

export function normalizeLanguage(value: string | null | undefined): LanguageCode {
  switch ((value ?? "").trim().toLowerCase()) {
    case "zh":
    case "zh-cn":
    case "zh_cn":
    case "cn":
    case "chinese":
    case "":
      return "zh-CN";
    case "en":
    case "en-us":
    case "en_us":
    case "english":
      return "en-US";
    case "ko":
    case "ko-kr":
    case "ko_kr":
    case "kr":
    case "korean":
      return "ko-KR";
    case "ja":
    case "ja-jp":
    case "ja_jp":
    case "jp":
    case "japanese":
      return "ja-JP";
    default:
      return defaultLanguage;
  }
}

