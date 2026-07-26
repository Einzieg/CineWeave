const CJK_CHARACTER = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/gu;
const WORD = /[\p{L}\p{N}]+(?:['’-][\p{L}\p{N}]+)*/gu;
const CJK_LOCALE = /^(zh|ja|ko)(-|$)/i;
const LATIN_LETTER = /\p{Script=Latin}/u;

export type CommerceScriptDraftMetrics = {
  units: number;
  unitLabel: "字" | "词";
  detectedLanguageLabel: "中文" | "英文" | "其他语言";
};

export function commerceScriptDraftMetrics(value: string, configuredLocale = ""): CommerceScriptDraftMetrics {
  const text = value.trim();
  if (!text) {
    return {
      units: 0,
      unitLabel: CJK_LOCALE.test(configuredLocale) ? "字" : "词",
      detectedLanguageLabel: CJK_LOCALE.test(configuredLocale) ? "中文" : "其他语言",
    };
  }

  const cjkCharacters = text.match(CJK_CHARACTER) ?? [];
  const words = text.match(WORD) ?? [];
  const latinWords = words.filter((word) => LATIN_LETTER.test(word));
  const countAsCJK = configuredLocale
    ? CJK_LOCALE.test(configuredLocale)
    : cjkCharacters.length > latinWords.length;

  if (countAsCJK) {
    return {
      units: cjkCharacters.length,
      unitLabel: "字",
      detectedLanguageLabel: "中文",
    };
  }

  return {
    units: words.length,
    unitLabel: "词",
    detectedLanguageLabel: latinWords.length > 0 ? "英文" : "其他语言",
  };
}
