import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Globe } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { showError } from '@/helpers/utils';
import { changeStudioLanguage, LANGUAGES } from '@/i18n';

export default function LanguageSwitcher() {
  const { i18n, t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const supported = i18n.options.supportedLngs;
  const languages = LANGUAGES.filter(({ code }) => supported && supported.includes(code));
  if (languages.length < 2) return null;

  const handleChange = async (code: string) => {
    setLoading(true);
    try {
      await changeStudioLanguage(code);
    } catch {
      showError(t('auth.language.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Select value={i18n.resolvedLanguage} onValueChange={handleChange} disabled={loading}>
      <SelectTrigger id='studio-language' size='sm' aria-label={t('auth.language.label')} aria-busy={loading} className='w-auto gap-1.5 px-2.5'>
        <Globe className='size-4 text-muted-foreground' />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {languages.map(({ code, label }) => (
          <SelectItem key={code} value={code}>
            <bdi lang={code}>{label}</bdi>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
