import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { storeUser } from '@/context/User';
import type { UserSelf } from './accountApi';
import RedeemTab from './RedeemTab';
import SettingsTab from './SettingsTab';
import TokensTab from './TokensTab';
import UsageTab from './UsageTab';

type AccountTab = 'tokens' | 'usage' | 'redeem' | 'settings';

// Account area: the user-facing account surface the split decision promised
// — API keys, personal usage, redemption-code top-up, and profile settings.
// Everything runs through owner-scoped endpoints that already existed; this
// page only wires them into the studio.
const Account: React.FC = () => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<AccountTab>('tokens');

  const tabs: { key: AccountTab; label: string }[] = [
    { key: 'tokens', label: t('studio.account.tabs.tokens') },
    { key: 'usage', label: t('studio.account.tabs.usage') },
    { key: 'redeem', label: t('studio.account.tabs.redeem') },
    { key: 'settings', label: t('studio.account.tabs.settings') },
  ];

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
            onClick={() => setTab(entry.key)}
          >
            {entry.label}
          </Button>
        ))}
      </div>
      {tab === 'tokens' && <TokensTab />}
      {tab === 'usage' && <UsageTab />}
      {tab === 'redeem' && <RedeemTab onSelfUpdated={storeUser} />}
      {tab === 'settings' && <SettingsTab onSelfUpdated={storeUser} />}
    </div>
  );
};

export default Account;
