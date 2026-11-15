import React from 'react';
import { useNavigate } from 'react-router-dom';
import { MessageCircle, Image as ImageIcon, Bot, Wrench, FolderOpen, CircleUserRound, ArrowRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { getSystemName } from '@/helpers/utils';

// Entry point per docs/studio-transformation-proposal.md §布局: the overview
// offers the task kinds; every confirmed entry is live — the image workbench
// shipped with the P3 pass.
const Home: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const entries = [
    { key: 'chat', icon: MessageCircle, onClick: () => navigate('/chat') },
    { key: 'work', icon: Wrench, onClick: () => navigate('/work') },
    { key: 'agent', icon: Bot, onClick: () => navigate('/agents') },
    { key: 'workspace', icon: FolderOpen, onClick: () => navigate('/workspace') },
    { key: 'image', icon: ImageIcon, onClick: () => navigate('/images') },
    { key: 'account', icon: CircleUserRound, onClick: () => navigate('/account') },
  ];

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-12'>
      <div className='mb-10'>
        <h1 className='text-3xl font-semibold tracking-tight'>{getSystemName()}</h1>
        <p className='text-muted-foreground mt-2 max-w-xl text-sm'>
          {t('studio.home.tagline')}
        </p>
      </div>
      <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
        {entries.map(({ key, icon: Icon, onClick }) => (
          <Card
            key={key}
            className='cursor-pointer transition-colors hover:border-foreground/30'
            onClick={onClick}
          >
            <CardHeader>
              <CardTitle className='flex items-center gap-2'>
                <Icon className='size-4' />
                {t(`studio.home.${key}.title`)}
              </CardTitle>
              <CardDescription>{t(`studio.home.${key}.description`)}</CardDescription>
              <CardAction>
                <Badge variant='secondary'>{t('studio.home.open')}</Badge>
              </CardAction>
            </CardHeader>
            <CardContent>
              <Button size='sm' variant='outline'>
                {t(`studio.home.${key}.cta`, { defaultValue: t('studio.home.enter') })}{' '}
                <ArrowRight />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
};

export default Home;
