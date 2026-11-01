import React from 'react';
import { useNavigate } from 'react-router-dom';
import { MessageCircle, Image as ImageIcon, Languages, Bot, Wrench, ArrowRight } from 'lucide-react';
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
// offers the four task kinds; only Chat is wired for this phase, the rest are
// marked 未开放 so the IA is visible without faking entries.
const Home: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const entries = [
    {
      key: 'chat',
      icon: MessageCircle,
      open: true,
      onClick: () => navigate('/chat'),
    },
    { key: 'work', icon: Wrench, open: true, onClick: () => navigate('/work') },
    { key: 'agent', icon: Bot, open: false },
    { key: 'image', icon: ImageIcon, open: false },
    { key: 'translate', icon: Languages, open: false },
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
        {entries.map(({ key, icon: Icon, open, onClick }) => (
          <Card
            key={key}
            className={
              open
                ? 'cursor-pointer transition-colors hover:border-foreground/30'
                : 'opacity-70'
            }
            onClick={open ? onClick : undefined}
          >
            <CardHeader>
              <CardTitle className='flex items-center gap-2'>
                <Icon className='size-4' />
                {t(`studio.home.${key}.title`)}
              </CardTitle>
              <CardDescription>{t(`studio.home.${key}.description`)}</CardDescription>
              <CardAction>
                {open ? (
                  <Badge variant='secondary'>{t('studio.home.open')}</Badge>
                ) : (
                  <Badge variant='outline'>{t('studio.home.comingSoon')}</Badge>
                )}
              </CardAction>
            </CardHeader>
            {open && (
              <CardContent>
                <Button size='sm' variant='outline'>
                  {t('studio.home.chat.cta')} <ArrowRight />
                </Button>
              </CardContent>
            )}
          </Card>
        ))}
      </div>
    </div>
  );
};

export default Home;
