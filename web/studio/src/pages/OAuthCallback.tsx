import React, { useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import { API } from '@/helpers/api';
import { showError, showSuccess } from '@/helpers/utils';
import { Card, CardContent } from '@/components/ui/card';

// Shared callback page for the OAuth providers the backend supports: the
// browser lands here with ?code=&state=, we exchange it for a session and
// store the user payload under the same key the admin console reads.
const OAuthCallback: React.FC<{
  provider: 'github' | 'lark';
  onSuccess: (data: unknown) => void;
}> = ({ provider, onSuccess }) => {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState('处理中...');
  const attempted = useRef(0);

  useEffect(() => {
    const sendCode = async (code: string | null, state: string | null) => {
      try {
        const res = await API.get(`/api/oauth/${provider}?code=${code}&state=${state}`);
        const { success, message, data } = res.data;
        if (success) {
          if (message === 'bind') {
            showSuccess('绑定成功！');
            navigate('/', { replace: true });
          } else {
            onSuccess(data);
            showSuccess('登录成功！');
            navigate('/', { replace: true });
          }
          return;
        }
        showError(message);
      } catch {
        // showError already toasted; fall through to the retry below
      }
      attempted.current += 1;
      if (attempted.current > 3) {
        setPrompt('操作失败，正在返回首页...');
        navigate('/', { replace: true });
        return;
      }
      setPrompt(`出现错误，第 ${attempted.current} 次重试中...`);
      await new Promise((resolve) => setTimeout(resolve, attempted.current * 2000));
      await sendCode(searchParams.get('code'), searchParams.get('state'));
    };
    sendCode(searchParams.get('code'), searchParams.get('state')).then();
  }, [provider, navigate, searchParams, onSuccess]);

  return (
    <div className='flex min-h-[calc(100vh-3.5rem)] items-center justify-center p-6'>
      <Card className='w-full max-w-sm'>
        <CardContent className='py-10 text-center text-sm text-muted-foreground'>
          {prompt}
        </CardContent>
      </Card>
    </div>
  );
};

export default OAuthCallback;
