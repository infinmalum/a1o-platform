import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { storeUser } from '@/context/User';
import PrivacyTab from './PrivacyTab';
import RedeemTab from './RedeemTab';
import SettingsTab from './SettingsTab';
import TokensTab from './TokensTab';
import UsageTab from './UsageTab';

type AccountTab = 'tokens' | 'usage' | 'redeem' | 'settings' | 'privacy';

// Account area: the user-facing account surface the split decision promised
// — API keys, personal usage, redemption-code top-up, and profile settings.
const Account: React.FC = () => {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();

  const tabs: { key: AccountTab; label: string }[] = [
    { key: 'tokens', label: t('studio.account.tabs.tokens') },
    { key: 'usage', label: t('studio.account.tabs.usage') },
    { key: 'redeem', label: t('studio.account.tabs.redeem') },
    { key: 'settings', label: t('studio.account.tabs.settings') },
    { key: 'privacy', label: t('studio.privacy.title') },
  ];

  const tab = tabs.find(({ key }) => key === params.get('tab'))?.key ?? 'tokens';

  return (
    <div className='mx-auto w-full max-w-4xl px-4 py-10'>
      <h1 className='mb-1 text-2xl font-semibold tracking-tight'>{t('studio.account.title')}</h1>
      <p className='text-muted-foreground mb-6 text-sm'>{t('studio.account.tagline')}</p>
      <div className='mb-6 flex flex-wrap gap-2'>
        {tabs.map((entry) => (
          <Button
            key={entry.key}
            variant={tab === entry.key ? 'default' : 'outline'}
            size='sm'
            aria-pressed={tab === entry.key}
            onClick={() => setParams((current) => {
              const next = new URLSearchParams(current);
              next.set('tab', entry.key);
              return next;
            })}
          >
            {entry.label}
          </Button>
        ))}
      </div>
      {tab === 'tokens' && <TokensTab />}
      {tab === 'usage' && <UsageTab />}
      {tab === 'redeem' && <RedeemTab onSelfUpdated={storeUser} />}
      {tab === 'settings' && <SettingsTab onSelfUpdated={storeUser} />}
      {tab === 'privacy' && <PrivacyTab />}
    </div>
  );
};

export default Account;
