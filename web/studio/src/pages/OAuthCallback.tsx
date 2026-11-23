import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { API } from '@/helpers/api';
import { safeReturnPath } from '@/helpers/session';
import { showSuccess } from '@/helpers/utils';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';

export default function OAuthCallback({ provider, onSuccess }: {
  provider: 'github' | 'lark';
  onSuccess: (data: unknown) => void;
}) {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [failed, setFailed] = useState(false);
  const exchange = useRef<Promise<{ success: boolean; message: string; data: unknown }> | null>(null);

  useEffect(() => {
    const code = searchParams.get('code');
    const state = searchParams.get('state');
    if (!code || !state) {
      setFailed(true);
      return;
    }
    // A provider code is single-use. Keep the request across effect replays.
    exchange.current ??= API.get('/api/oauth/' + provider, { params: { code, state } }).then((res) => res.data);
    let active = true;
    void exchange.current.then(({ success, message, data }) => {
      if (!active) return;
      if (!success) {
        setFailed(true);
        return;
      }
      const next = sessionStorage.getItem('studio-oauth-return');
      sessionStorage.removeItem('studio-oauth-return');
      if (message === 'bind') {
        showSuccess(t('auth.oauth.bound'));
        navigate('/', { replace: true });
      } else {
        onSuccess(data);
        showSuccess(t('messages.success.login'));
        navigate(next ? safeReturnPath(next) : '/', { replace: true });
      }
    }).catch(() => { if (active) setFailed(true); });
    return () => { active = false; };
  }, [provider, navigate, searchParams, onSuccess, t]);

  return (
    <div className='flex min-h-[calc(100vh-3.5rem)] items-center justify-center p-6'>
      <Card className='w-full max-w-sm'>
        <CardContent className='grid justify-items-center gap-4 py-10 text-center text-sm text-muted-foreground'>
          <p role={failed ? 'alert' : 'status'}>{t(failed ? 'auth.oauth.failed' : 'auth.oauth.processing')}</p>
          {failed && <Button asChild variant='outline'><Link to='/login'>{t('auth.login.button')}</Link></Button>}
        </CardContent>
      </Card>
    </div>
  );
}
