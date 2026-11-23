import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Gift, Loader2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { renderQuota } from '@/helpers/render';
import { showSuccess } from '@/helpers/utils';
import PaymentPackages from './PaymentPackages';
import { accountApi, type UserSelf } from './accountApi';

// Redemption tab: the user-facing end of the operator's quota distribution.
// A redemption code carries its quota to the submitting account; the backend
// enforces one-time use and ownership.
const RedeemTab: React.FC<{ onSelfUpdated: (self: UserSelf) => void }> = ({ onSelfUpdated }) => {
  const { t } = useTranslation();
  const [key, setKey] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [granted, setGranted] = useState<number | null>(null);

  const submit = async () => {
    const trimmed = key.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      const quota = await accountApi.redeem(trimmed);
      setGranted(quota);
      setKey('');
      showSuccess(t('studio.account.redeem.success', { quota: renderQuota(quota, t) }));
      const fresh = await accountApi.getSelf();
      onSelfUpdated(fresh);
    } catch {
      // error toast already surfaced by the request helper
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className='space-y-4'>
      <PaymentPackages onSelfUpdated={onSelfUpdated} />
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <Gift className='size-4' />
            {t('studio.account.redeem.title')}
          </CardTitle>
          <CardDescription>{t('studio.account.redeem.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='space-y-1.5'>
            <Label htmlFor='redeem-key'>{t('studio.account.redeem.codeLabel')}</Label>
            <Input
              id='redeem-key'
              value={key}
              placeholder={t('studio.account.redeem.codePlaceholder')}
              onChange={(e) => {
                setKey(e.target.value);
                setGranted(null);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && key.trim() && !submitting) void submit();
              }}
            />
          </div>
          {granted !== null && (
            <p className='text-sm'>
              {t('studio.account.redeem.granted', { quota: renderQuota(granted, t) })}
            </p>
          )}
          <Button onClick={() => void submit()} disabled={!key.trim() || submitting}>
            {submitting && <Loader2 className='size-4 animate-spin' />}
            {t('studio.account.redeem.submit')}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
};

export default RedeemTab;
