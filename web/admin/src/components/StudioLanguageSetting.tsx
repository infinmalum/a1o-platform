import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Header, Message } from 'semantic-ui-react';
import { API, showError, showSuccess } from '../helpers';

interface StudioLanguage {
  code: string;
  enabled: boolean;
}

export default function StudioLanguageSetting() {
  const { t } = useTranslation();
  const [languages, setLanguages] = useState<StudioLanguage[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const request = new AbortController();
    API.get('/api/status', { signal: request.signal })
      .then(({ data }) => {
        if (data.success && Array.isArray(data.data?.studio_languages)) {
          setLanguages(data.data.studio_languages);
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (!request.signal.aborted) setLoading(false);
      });
    return () => request.abort();
  }, []);

  const enabled = languages.filter((language) => language.enabled).length;

  async function save() {
    setSaving(true);
    try {
      const value = languages.filter((language) => !language.enabled).map(({ code }) => code).join(',');
      const { data } = await API.put('/api/option/', { key: 'StudioDisabledLanguages', value });
      if (data.success) showSuccess(t('setting.studio_languages.saved'));
      else showError(t('setting.studio_languages.save_failed'));
    } catch {
      // The shared API interceptor reports request failures.
    } finally {
      setSaving(false);
    }
  }

  return (
    <Form loading={loading || saving} onSubmit={save}>
      <Header as='h3'>{t('setting.tabs.studio_languages')}</Header>
      <p>{t('setting.studio_languages.description')}</p>
      {!loading && languages.length === 0 && <Message negative content={t('setting.studio_languages.load_failed')} />}
      {languages.map(({ code, enabled: checked }) => (
        <Form.Checkbox
          key={code}
          id={'studio-language-' + code}
          label={new Intl.DisplayNames([code], { type: 'language' }).of(code)}
          checked={checked}
          disabled={saving || (checked && enabled === 1)}
          onChange={(_, { checked }) => setLanguages((current) => current.map((language) =>
            language.code === code ? { ...language, enabled: Boolean(checked) } : language
          ))}
        />
      ))}
      <Form.Button type='submit' disabled={enabled === 0 || saving}>{t('setting.studio_languages.save')}</Form.Button>
    </Form>
  );
}
