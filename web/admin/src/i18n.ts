import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import zhTranslation from './locales/zh/translation.json';
import enTranslation from './locales/en/translation.json';

const supportedLngs = ['zh', 'en'];
const normalize = (locale: string) => locale.split(/[-_]/, 1)[0].toLowerCase();
const detector = new LanguageDetector();

detector.addDetector({
  name: 'previousPreference',
  lookup() {
    // Honor existing Chinese or English preferences without letting either
    // interface overwrite the other's preference on the shared origin.
    try {
      const previous = normalize(localStorage.getItem('i18nextLng') ?? '');
      return supportedLngs.includes(previous) ? previous : undefined;
    } catch {
      return undefined;
    }
  },
});

i18n.on('languageChanged', () => {
  document.documentElement.lang = i18n.resolvedLanguage ?? 'en';
});

void i18n
  .use(detector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs,
    load: 'languageOnly',
    debug: import.meta.env.MODE === 'development',
    detection: {
      order: ['querystring', 'localStorage', 'previousPreference', 'navigator'],
      lookupLocalStorage: 'a1o-admin-language',
      caches: ['localStorage'],
      convertDetectedLanguage: normalize,
    },
    interpolation: { escapeValue: false },
    resources: {
      zh: { translation: zhTranslation },
      en: { translation: enTranslation },
    },
  });

export default i18n;
