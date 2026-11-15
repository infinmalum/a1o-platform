import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Loader2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { renderQuota, renderNumber } from '@/helpers/render';
import { accountApi, type LogStatistic, type UsageLog } from './accountApi';

// Personal usage tab: the caller's spend, the last seven days by day and by
// model, and the recent consume log. Data comes from the same endpoints the
// admin console's log views use, scoped to the session user server-side.
const UsageTab: React.FC = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [usedQuota, setUsedQuota] = useState<number | null>(null);
  const [stats, setStats] = useState<LogStatistic[]>([]);
  const [logs, setLogs] = useState<UsageLog[]>([]);
  const [page, setPage] = useState(0);

  const reload = useCallback(
    async (targetPage: number) => {
      setLoading(true);
      try {
        const [quota, dashboard, logRows] = await Promise.all([
          accountApi.getUsedQuota(),
          accountApi.getDashboard(),
          accountApi.listLogs(targetPage),
        ]);
        setUsedQuota(quota.quota);
        setStats(dashboard);
        setLogs(logRows);
        setPage(targetPage);
      } finally {
        setLoading(false);
      }
    },
    []
  );

  useEffect(() => {
    void reload(0);
  }, [reload]);

  const byDay = useMemo(() => {
    const map = new Map<string, number>();
    for (const row of stats) {
      map.set(row.day, (map.get(row.day) ?? 0) + row.quota);
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [stats]);

  const byModel = useMemo(() => {
    const map = new Map<string, { quota: number; count: number }>();
    for (const row of stats) {
      const entry = map.get(row.model_name) ?? { quota: 0, count: 0 };
      entry.quota += row.quota;
      entry.count += row.request_count;
      map.set(row.model_name, entry);
    }
    return [...map.entries()].sort(([, a], [, b]) => b.quota - a.quota).slice(0, 8);
  }, [stats]);

  const maxDayQuota = Math.max(1, ...byDay.map(([, quota]) => quota));
  const maxModelQuota = Math.max(1, ...byModel.map(([, entry]) => entry.quota));

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('studio.account.usage.title')}</CardTitle>
          <CardDescription>{t('studio.account.usage.description')}</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className='text-muted-foreground flex items-center justify-center py-10'>
              <Loader2 className='size-4 animate-spin' />
            </div>
          ) : (
            <div className='space-y-6'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='rounded-lg border p-4'>
                  <p className='text-muted-foreground text-xs'>{t('studio.account.usage.totalSpend')}</p>
                  <p className='mt-1 text-2xl font-semibold'>
                    {usedQuota === null ? '—' : renderQuota(usedQuota, t)}
                  </p>
                </div>
                <div className='rounded-lg border p-4'>
                  <p className='text-muted-foreground text-xs'>{t('studio.account.usage.recentRequests')}</p>
                  <p className='mt-1 text-2xl font-semibold'>
                    {renderNumber(stats.reduce((sum, row) => sum + row.request_count, 0))}
                  </p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t('studio.account.usage.recentWindow')}
                  </p>
                </div>
              </div>

              <div>
                <h3 className='mb-2 text-sm font-medium'>{t('studio.account.usage.daily')}</h3>
                {byDay.length === 0 ? (
                  <p className='text-muted-foreground text-sm'>{t('studio.account.usage.noData')}</p>
                ) : (
                  <div className='space-y-1.5'>
                    {byDay.map(([day, quota]) => (
                      <div key={day} className='flex items-center gap-2 text-xs'>
                        <span className='text-muted-foreground w-24 shrink-0'>{day}</span>
                        <div className='bg-secondary h-2.5 flex-1 rounded-full'>
                          <div
                            className='bg-primary h-2.5 rounded-full'
                            style={{ width: `${Math.max(2, (quota / maxDayQuota) * 100)}%` }}
                          />
                        </div>
                        <span className='w-24 shrink-0 text-right'>{renderQuota(quota, t)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div>
                <h3 className='mb-2 text-sm font-medium'>{t('studio.account.usage.byModel')}</h3>
                {byModel.length === 0 ? (
                  <p className='text-muted-foreground text-sm'>{t('studio.account.usage.noData')}</p>
                ) : (
                  <div className='space-y-1.5'>
                    {byModel.map(([model, entry]) => (
                      <div key={model} className='flex items-center gap-2 text-xs'>
                        <span className='w-40 shrink-0 truncate font-mono'>{model}</span>
                        <div className='bg-secondary h-2.5 flex-1 rounded-full'>
                          <div
                            className='bg-primary h-2.5 rounded-full'
                            style={{ width: `${Math.max(2, (entry.quota / maxModelQuota) * 100)}%` }}
                          />
                        </div>
                        <span className='w-24 shrink-0 text-right'>{renderQuota(entry.quota, t)}</span>
                        <span className='text-muted-foreground w-14 shrink-0 text-right'>
                          {t('studio.account.usage.requestCount', { count: entry.count })}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='text-base'>{t('studio.account.usage.logsTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          {logs.length === 0 ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('studio.account.usage.logsEmpty')}
            </p>
          ) : (
            <>
              <div className='overflow-x-auto'>
                <table className='w-full text-sm'>
                  <thead>
                    <tr className='text-muted-foreground border-b text-left'>
                      <th className='py-2 pr-3 font-medium'>{t('studio.account.usage.time')}</th>
                      <th className='py-2 pr-3 font-medium'>{t('studio.account.usage.model')}</th>
                      <th className='py-2 pr-3 font-medium'>{t('studio.account.usage.tokens')}</th>
                      <th className='py-2 font-medium'>{t('studio.account.usage.spend')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {logs.map((log) => (
                      <tr key={log.id} className='border-b last:border-0'>
                        <td className='text-muted-foreground py-2 pr-3 whitespace-nowrap'>
                          {new Date(log.created_at * 1000).toLocaleString()}
                        </td>
                        <td className='py-2 pr-3 font-mono text-xs'>
                          {log.model_name || '—'}
                          {log.token_name ? (
                            <span className='text-muted-foreground ml-2'>({log.token_name})</span>
                          ) : null}
                        </td>
                        <td className='py-2 pr-3 whitespace-nowrap'>
                          {renderNumber(log.prompt_tokens)} / {renderNumber(log.completion_tokens)}
                        </td>
                        <td className='py-2 whitespace-nowrap'>{renderQuota(log.quota, t)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className='mt-3 flex items-center justify-end gap-2'>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={page === 0}
                  onClick={() => void reload(page - 1)}
                >
                  {t('studio.account.usage.prevPage')}
                </Button>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={logs.length === 0}
                  onClick={() => void reload(page + 1)}
                >
                  {t('studio.account.usage.nextPage')}
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default UsageTab;
