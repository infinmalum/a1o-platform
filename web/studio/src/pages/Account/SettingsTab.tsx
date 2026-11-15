import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Copy, Loader2, RefreshCcw } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { renderQuota } from '@/helpers/render';
import { showError, showSuccess } from '@/helpers/utils';
import { storeUser } from '@/context/User';
import { accountApi, type UserSelf } from './accountApi';

// Profile tab: display identity, password change, and the system access
// token used for management API calls. Quota and group are read-only here —
// operators change them from the admin console or a redemption code.
const SettingsTab: React.FC<{ onSelfUpdated: (self: UserSelf) => void }> = ({ onSelfUpdated }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [self, setSelf] = useState<UserSelf | null>(null);
  const [copiedToken, setCopiedToken] = useState(false);
  const [generatingToken, setGeneratingToken] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        const loaded = await accountApi.getSelf();
        setSelf(loaded);
        setUsername(loaded.username);
        setDisplayName(loaded.display_name ?? '');
        onSelfUpdated(loaded);
      } finally {
        setLoading(false);
      }
    })();
    // onSelfUpdated is a stable setState-shaped callback from the parent.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const save = async () => {
    if (!username.trim()) {
      showError(t('studio.account.settings.usernameRequired'));
      return;
    }
    if (password && (password.length < 8 || password.length > 20)) {
      showError(t('studio.account.settings.passwordLength'));
      return;
    }
    setSaving(true);
    try {
      await accountApi.updateSelf({
        username: username.trim(),
        display_name: displayName.trim(),
        ...(password ? { password } : {}),
      });
      const fresh = await accountApi.getSelf();
      setSelf(fresh);
      onSelfUpdated(fresh);
      storeUser(fresh);
      setPassword('');
      showSuccess(t('studio.account.settings.saveSuccess'));
    } catch {
      // error toast already surfaced by the request helper
    } finally {
      setSaving(false);
    }
  };

  const regenerateToken = async () => {
    setGeneratingToken(true);
    try {
      const token = await accountApi.generateAccessToken();
      const fresh = self ? { ...self, access_token: token } : self;
      setSelf(fresh);
      if (fresh) {
        storeUser(fresh);
        onSelfUpdated(fresh);
      }
      showSuccess(t('studio.account.settings.tokenGenerated'));
    } catch {
      // error toast already surfaced by the request helper
    } finally {
      setGeneratingToken(false);
    }
  };

  const copyToken = async () => {
    if (!self?.access_token) return;
    await navigator.clipboard.writeText(self.access_token);
    setCopiedToken(true);
    window.setTimeout(() => setCopiedToken(false), 1500);
  };

  if (loading) {
    return (
      <Card>
        <CardContent className='text-muted-foreground flex items-center justify-center py-10'>
          <Loader2 className='size-4 animate-spin' />
        </CardContent>
      </Card>
    );
  }

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('studio.account.settings.title')}</CardTitle>
          <CardDescription>{t('studio.account.settings.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='space-y-1.5'>
            <Label htmlFor='settings-username'>{t('studio.account.settings.username')}</Label>
            <Input
              id='settings-username'
              value={username}
              maxLength={12}
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='settings-display'>{t('studio.account.settings.displayName')}</Label>
            <Input
              id='settings-display'
              value={displayName}
              maxLength={20}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='settings-password'>{t('studio.account.settings.newPassword')}</Label>
            <Input
              id='settings-password'
              type='password'
              value={password}
              placeholder={t('studio.account.settings.newPasswordPlaceholder')}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className='text-muted-foreground flex flex-wrap gap-x-6 gap-y-1 text-sm'>
            <span>
              {t('studio.account.settings.balance')}: {renderQuota(self?.quota ?? 0, t)}
            </span>
            <span>
              {t('studio.account.settings.group')}: {self?.group ?? 'default'}
            </span>
            <span>
              {t('studio.account.settings.requestCount')}: {self?.request_count ?? 0}
            </span>
          </div>
          <Button onClick={() => void save()} disabled={saving}>
            {saving && <Loader2 className='size-4 animate-spin' />}
            {t('studio.account.save')}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='text-base'>{t('studio.account.settings.accessTokenTitle')}</CardTitle>
          <CardDescription>{t('studio.account.settings.accessTokenDescription')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-3'>
          {self?.access_token ? (
            <div className='flex items-center gap-2'>
              <code className='bg-muted flex-1 truncate rounded px-2 py-1 font-mono text-sm'>
                {self.access_token}
              </code>
              <Button variant='outline' size='icon-sm' onClick={() => void copyToken()} title={t('studio.account.tokens.copy')}>
                {copiedToken ? <Check className='size-4' /> : <Copy className='size-4' />}
              </Button>
            </div>
          ) : (
            <p className='text-muted-foreground text-sm'>{t('studio.account.settings.noAccessToken')}</p>
          )}
          <Button variant='outline' onClick={() => void regenerateToken()} disabled={generatingToken}>
            {generatingToken ? <Loader2 className='size-4 animate-spin' /> : <RefreshCcw className='size-4' />}
            {t('studio.account.settings.regenerateToken')}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
};

export default SettingsTab;
