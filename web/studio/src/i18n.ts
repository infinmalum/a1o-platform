import i18n, { type BackendModule } from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import zhTranslation from './locales/zh.json';
import type { StudioStatus } from './helpers/studioStatus';

export const LANGUAGES = [
  { code: 'zh', label: '中文' },
  { code: 'en', label: 'English' },
  { code: 'fr', label: 'Français' },
  { code: 'de', label: 'Deutsch' },
  { code: 'es', label: 'Español' },
  { code: 'ru', label: 'Русский' },
  { code: 'uk', label: 'Українська' },
  { code: 'be', label: 'Беларуская' },
  { code: 'fa', label: 'فارسی' },
  { code: 'ar', label: 'العربية' },
] as const;

const defaultLanguage = 'zh';
const translations = import.meta.glob<typeof zhTranslation>(['./locales/*.json', '!./locales/zh.json'], { import: 'default' });

async function readLocale(language: string) {
  const load = translations[`./locales/${language}.json`];
  if (!load) throw new Error(`Unknown Studio locale: ${language}`);
  return load();
}

const localeBackend: BackendModule = {
  type: 'backend',
  init() {},
  read(language, _namespace, callback) {
    void readLocale(language).then(
      (translation) => callback(null, translation),
      (error: Error) => callback(error, null),
    );
  },
};

export async function changeStudioLanguage(language: string) {
  if (!i18n.options.supportedLngs || !i18n.options.supportedLngs.includes(language)) return;
  // Keep the current language and its saved preference if loading fails.
  if (!i18n.hasResourceBundle(language, 'translation')) {
    i18n.addResourceBundle(language, 'translation', await readLocale(language));
  }
  await i18n.changeLanguage(language);
}

i18n.on('languageChanged', () => {
  const language = i18n.resolvedLanguage ?? defaultLanguage;
  document.documentElement.lang = language;
  document.documentElement.dir = i18n.dir(language);
});

export function initializeStudioLanguage(status: StudioStatus | null) {
  const enabled = LANGUAGES
    .filter(({ code }) => status?.studio_languages?.some((language) => language.code === code && language.enabled))
    .map(({ code }) => code);
  const supportedLngs = enabled.length ? enabled : LANGUAGES.map(({ code }) => code);

  return i18n
    .use(LanguageDetector)
    .use(localeBackend)
    .use(initReactI18next)
    .init({
      fallbackLng: supportedLngs[0],
      supportedLngs,
      load: 'languageOnly',
      partialBundledLanguages: true,
      debug: import.meta.env.MODE === 'development',
      detection: {
        order: ['querystring', 'cookie', 'localStorage', 'sessionStorage', 'navigator'],
        caches: ['localStorage'],
        convertDetectedLanguage: (locale) => locale.split(/[-_]/, 1)[0].toLowerCase(),
      },
      interpolation: { escapeValue: false },
      resources: { zh: { translation: zhTranslation } },
    });
}

export default i18n;
