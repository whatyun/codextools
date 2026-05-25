export type LanguageCode = "zh-CN" | "en-US" | "ko-KR" | "ja-JP";

export type LanguageOption = {
  code: LanguageCode;
  nativeName: string;
  englishName: string;
};

export type TranslationDictionary = Record<string, string>;

