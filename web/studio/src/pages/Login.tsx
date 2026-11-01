import React, { useEffect, useState } from 'react';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';

import { API } from '@/helpers/api';
import { getLogo, getSystemName, showError, showSuccess } from '@/helpers/utils';
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

const Login: React.FC = () => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({ username: '', password: '' });
  const [searchParams] = useSearchParams();
  const [submitted, setSubmitted] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();

  // PrivateRoute records the page that bounced the visitor to /login, so a
  // sign-in from /chat returns to /chat instead of the home page.
  const redirectAfterLogin = () => {
    const from = (location.state as { from?: { pathname?: string; search?: string } } | null)?.from;
    if (from?.pathname && from.pathname !== '/login') {
      navigate(`${from.pathname}${from.search ?? ''}`, { replace: true });
      return;
    }
    navigate('/chat');
  };

  useEffect(() => {
    if (searchParams.get('expired')) {
      showError(t('messages.error.login_expired'));
    }
  }, [searchParams, t]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (!inputs.username || !inputs.password) {
      return;
    }
    try {
      const res = await API.post('/api/user/login', {
        username: inputs.username,
        password: inputs.password,
      });
      const { success, message, data } = res.data;
      if (success) {
        storeUser(data);
        showSuccess(t('messages.success.login'));
        redirectAfterLogin();
      } else {
        showError(message);
      }
    } catch {
      // the interceptor toasts the error
    }
  };

  return (
    <div className='flex min-h-[calc(100vh-3.5rem)] items-center justify-center p-6'>
      <Card className='w-full max-w-sm'>
        <CardHeader className='text-center'>
          <img
            src={getLogo()}
            alt='logo'
            className='mx-auto mb-2 size-16 object-contain'
          />
          <CardTitle className='text-xl'>{t('auth.login.title')}</CardTitle>
          <CardDescription>{getSystemName()}</CardDescription>
        </CardHeader>
        <CardContent>
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
            <Button type='submit' disabled={submitted} className='w-full'>
              {t('auth.login.button')}
            </Button>
          </form>
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
