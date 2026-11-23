import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';

export const studioPageSize = 12;

export function pageQuery(page: number) {
  return new URLSearchParams({ offset: String(page * studioPageSize), limit: String(studioPageSize + 1) });
}

export function useStudioPage<T>(read: (page: number) => Promise<T[] | null>) {
  const [page, setPage] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ items: T[]; failed: boolean; page: number; attempt: number }>();
  const current = result?.page === page && result.attempt === attempt ? result : undefined;

  useEffect(() => {
    let active = true;
    void read(page).then((items) => {
      if (!active) return;
      if (!items?.length && page > 0) {
        setPage(page - 1);
        return;
      }
      setResult({ items: items ?? [], failed: false, page, attempt });
    }).catch(() => { if (active) setResult({ items: [], failed: true, page, attempt }); });
    return () => { active = false; };
  }, [read, page, attempt]);

  const reload = useCallback(() => setAttempt((current) => current + 1), []);
  return {
    page, setPage, reload, loading: !current, failed: current?.failed ?? false,
    items: current?.items.slice(0, studioPageSize) ?? [],
    more: (current?.items.length ?? 0) > studioPageSize,
  };
}

export function ListLoading({ cards = false }: { cards?: boolean }) {
  const { t } = useTranslation();
  return (
    <div role='status' aria-label={t('studio.list.loading')}
      className={cards ? 'mb-4 grid gap-3 sm:grid-cols-3' : 'mb-4 grid gap-2'}>
      {[0, 1, 2].map((item) => (
        <div aria-hidden='true' key={item} className={'rounded-md bg-muted motion-safe:animate-pulse ' + (cards ? 'h-24' : 'h-8')} />
      ))}
    </div>
  );
}

export function ListError({ retry }: { retry: () => void }) {
  const { t } = useTranslation();
  return (
    <div className='mb-4 flex flex-wrap items-center gap-3 rounded-md border px-3 py-2 text-sm' role='alert'>
      <p className='text-destructive'>{t('studio.list.failed')}</p>
      <Button size='sm' variant='outline' onClick={retry}>{t('studio.list.retry')}</Button>
    </div>
  );
}

export function ListPages({ page, more, onPage }: { page: number; more: boolean; onPage: (page: number) => void }) {
  const { t } = useTranslation();
  if (!page && !more) return null;
  return (
    <nav className='my-3 flex flex-wrap items-center gap-2' aria-label={t('studio.list.pages')}>
      <Button size='sm' variant='outline' disabled={!page} onClick={() => onPage(page - 1)}>{t('studio.list.previous')}</Button>
      <span className='text-muted-foreground text-sm'>{t('studio.list.page', { page: page + 1 })}</span>
      <Button size='sm' variant='outline' disabled={!more} onClick={() => onPage(page + 1)}>{t('studio.list.next')}</Button>
    </nav>
  );
}
