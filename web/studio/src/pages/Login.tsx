import React, { useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { API } from '@/helpers/api';
import { safeReturnPath } from '@/helpers/session';
import type { StudioStatus } from '@/helpers/studioStatus';
import { getLogo, getSystemName, showSuccess } from '@/helpers/utils';
import { storeUser } from '@/context/User';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

const Login: React.FC<{ status: StudioStatus | null }> = ({ status }) => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({ username: '', password: '' });
  const [searchParams] = useSearchParams();
  const [submitted, setSubmitted] = useState(false);
  const [error, setError] = useState('');
  const [oauthBusy, setOAuthBusy] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();

  const from = (location.state as { from?: { pathname: string; search: string; hash: string } } | null)?.from;
  const returnPath = safeReturnPath(from ? from.pathname + from.search + from.hash : searchParams.get('next'));

  const startOAuth = async (provider: 'github' | 'lark', clientId: string) => {
    if (oauthBusy || submitted) return;
    setOAuthBusy(true);
    setError('');
    try {
      const { data } = await API.get('/api/oauth/state');
      if (!data?.success || typeof data.data !== 'string' || !data.data) throw new Error('OAuth state unavailable');
      sessionStorage.setItem('studio-oauth-return', returnPath);
      const url = new URL(provider === 'github'
        ? 'https://github.com/login/oauth/authorize'
        : 'https://open.feishu.cn/open-apis/authen/v1/index');
      url.searchParams.set('state', data.data);
      if (provider === 'github') {
        url.searchParams.set('client_id', clientId);
        url.searchParams.set('scope', 'user:email');
      } else {
        url.searchParams.set('app_id', clientId);
        url.searchParams.set('redirect_uri', window.location.origin + '/oauth/lark');
      }
      window.location.assign(url);
    } catch {
      setError(t('auth.oauth.failed'));
      setOAuthBusy(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitted || oauthBusy) return;
    if (!inputs.username.trim() || !inputs.password) {
      setError(t('auth.login.required'));
      return;
    }
    setError('');
    setSubmitted(true);
    try {
      const res = await API.post('/api/user/login', {
        username: inputs.username,
        password: inputs.password,
      });
      const { success, data, code } = res.data;
      if (success) {
        storeUser(data);
        showSuccess(t('messages.success.login'));
        navigate(returnPath, { replace: true });
      } else {
        setError(t(code === 'password_login_disabled' ? 'auth.login.passwordDisabled' : 'auth.login.rejected'));
      }
    } catch {
      setError(t('auth.login.failed'));
    } finally {
      setSubmitted(false);
    }
  };

  return (
    <div className='flex min-h-[calc(100vh-3.5rem)] items-center justify-center p-6'>
      <Card className='w-full max-w-sm'>
        <CardHeader className='text-center'>
          <img
            src={getLogo()}
            alt=''
            className='mx-auto mb-2 size-16 object-contain'
          />
          <CardTitle className='text-xl'>{t('auth.login.title')}</CardTitle>
          <CardDescription>{getSystemName()}</CardDescription>
        </CardHeader>
        <CardContent>
          {searchParams.has('expired') && (
            <p className='text-muted-foreground mb-3 text-sm' role='status'>{t('messages.error.login_expired')}</p>
          )}
          <form className='grid gap-4' onSubmit={handleSubmit}>
            <div className='grid gap-2'>
              <Label htmlFor='username'>{t('auth.login.username')}</Label>
              <Input
                id='username'
                name='username'
                autoComplete='username'
                value={inputs.username}
                onChange={(e) => setInputs({ ...inputs, username: e.target.value })}
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='password'>{t('auth.login.password')}</Label>
              <Input
                id='password'
                name='password'
                type='password'
                autoComplete='current-password'
                value={inputs.password}
                onChange={(e) => setInputs({ ...inputs, password: e.target.value })}
              />
            </div>
            <Button type='submit' disabled={submitted || oauthBusy} className='w-full'>
              {t(submitted ? 'auth.login.submitting' : 'auth.login.button')}
            </Button>
            {error && <p className='text-destructive text-sm' role='alert'>{error}</p>}
          </form>
          {((status?.github_oauth && status.github_client_id) || status?.lark_client_id) && (
            <div className='mt-4 grid gap-2 border-t pt-4'>
              {status.github_oauth && status.github_client_id && (
                <Button variant='outline' disabled={submitted || oauthBusy}
                  onClick={() => void startOAuth('github', status.github_client_id!)}>{t('auth.oauth.github')}</Button>
              )}
              {status.lark_client_id && (
                <Button variant='outline' disabled={submitted || oauthBusy}
                  onClick={() => void startOAuth('lark', status.lark_client_id!)}>{t('auth.oauth.lark')}</Button>
              )}
            </div>
          )}
          <p className='text-muted-foreground mt-4 text-center text-xs'>
            <a className='underline underline-offset-4' href='/admin/register'>
              {t('auth.login.register')}
            </a>
            {' · '}
            <a className='underline underline-offset-4' href='/admin/reset'>
              {t('auth.login.forgot_password')}
            </a>
          </p>
        </CardContent>
      </Card>
    </div>
  );
};

export default Login;
