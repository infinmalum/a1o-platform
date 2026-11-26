import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Checkbox, Space, Spin, Typography } from 'antd';
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

  if (loading) {
    return <Spin />;
  }

  return (
    <div>
      <Typography.Title level={5}>
        {t('setting.tabs.studio_languages')}
      </Typography.Title>
      <Typography.Paragraph type='secondary'>
        {t('setting.studio_languages.description')}
      </Typography.Paragraph>
      {!loading && languages.length === 0 && (
        <Alert
          type='error'
          showIcon
          message={t('setting.studio_languages.load_failed')}
          style={{ marginBottom: 12 }}
        />
      )}
      <Space direction='vertical' style={{ display: 'flex', marginBottom: 16 }}>
        {languages.map(({ code, enabled: checked }) => (
          <Checkbox
            key={code}
            checked={checked}
            disabled={saving || (checked && enabled === 1)}
            onChange={(e) =>
              setLanguages((current) =>
                current.map((language) =>
                  language.code === code
                    ? { ...language, enabled: e.target.checked }
                    : language
                )
              )
            }
          >
            {new Intl.DisplayNames([code], { type: 'language' }).of(code)}
          </Checkbox>
        ))}
      </Space>
      <div>
        <Button
          type='primary'
          disabled={enabled === 0 || saving}
          loading={saving}
          onClick={save}
        >
          {t('setting.studio_languages.save')}
        </Button>
      </div>
    </div>
  );
}
