import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { clearUser, useUser } from '@/context/User';
import { studioRequest } from '@/helpers/studioRequest';

const memoryURL = '/api/user/privacy/memory';

type MemoryEntry = { id: string; name: string; content: string };
type Memory = { available: boolean; enabled: boolean; entries: MemoryEntry[] };
type Removal = { kind: 'account' | 'all' } | { kind: 'entry'; id: string };

export default function PrivacyTab() {
  const { t } = useTranslation();
  const { user } = useUser();
  const [memory, setMemory] = useState<Memory | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [remove, setRemove] = useState<Removal | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const result = await studioRequest<Memory>(memoryURL, { signal }, false);
      if (!signal?.aborted) {
        setMemory(result);
        setLoadFailed(false);
      }
    } catch {
      if (!signal?.aborted) setLoadFailed(true);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function setEnabled(enabled: boolean) {
    setBusy(true);
    try {
      await studioRequest(memoryURL, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled }),
      });
      await load();
    } catch {
      // The request helper reports mutation failures.
    } finally {
      setBusy(false);
    }
  }

  async function confirmRemoval() {
    if (!remove) return;
    setBusy(true);
    try {
      if (remove.kind === 'account') {
        await studioRequest('/api/user/self', { method: 'DELETE' });
        clearUser();
        window.location.assign('/login');
        return;
      }
      const url = remove.kind === 'entry' ? memoryURL + '/' + encodeURIComponent(remove.id) : memoryURL;
      await studioRequest(url, { method: 'DELETE' });
      setRemove(null);
      await load();
    } catch {
      // Keep the confirmation open if the operation fails.
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader><CardTitle>{t('studio.privacy.memory')}</CardTitle></CardHeader>
        <CardContent className='space-y-4'>
          <p className='text-sm text-muted-foreground'>{t('studio.privacy.memoryDescription')}</p>
          {loadFailed && (
            <Button variant='outline' disabled={busy} onClick={() => void load()}>{t('studio.list.retry')}</Button>
          )}
          {!memory && !loadFailed && <p role='status'>{t('studio.list.loading')}</p>}
          {memory && (
            <>
              <label className='flex items-center gap-2 text-sm'>
                <input
                  type='checkbox'
                  checked={memory.enabled}
                  disabled={busy || (!memory.available && !memory.enabled)}
                  onChange={(event) => void setEnabled(event.target.checked)}
                />
                {t('studio.privacy.enableMemory')}
              </label>
              {!memory.available && (
                <p className='text-sm text-muted-foreground'>{t('studio.privacy.memoryUnavailable')}</p>
              )}
              {memory.entries.length ? (
                <>
                  <ul className='divide-y'>
                    {memory.entries.map((entry) => (
                      <li className='space-y-2 py-2' key={entry.id}>
                        <details>
                          <summary className='cursor-pointer font-medium break-words'>{entry.name}</summary>
                          <p className='mt-2 whitespace-pre-wrap break-words text-sm' dir='auto'>{entry.content}</p>
                        </details>
                        <Button size='sm' variant='outline' disabled={busy} onClick={() => setRemove({ kind: 'entry', id: entry.id })}>
                          {t('studio.privacy.deleteMemory')}
                        </Button>
                      </li>
                    ))}
                  </ul>
                  <Button variant='outline' disabled={busy} onClick={() => setRemove({ kind: 'all' })}>
                    {t('studio.privacy.clearMemory')}
                  </Button>
                </>
              ) : (
                <p className='text-sm text-muted-foreground'>{t('studio.privacy.noMemory')}</p>
              )}
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{t('studio.privacy.data')}</CardTitle></CardHeader>
        <CardContent className='space-y-3'>
          <Button asChild variant='outline'>
            <a href='/api/user/privacy/export' download>{t('studio.privacy.export')}</a>
          </Button>
          <p className='text-sm text-muted-foreground'>{t('studio.privacy.retention')}</p>
          {user?.role !== undefined && user.role < 100 && (
            <Button variant='destructive' disabled={busy} onClick={() => setRemove({ kind: 'account' })}>
              {t('studio.privacy.deleteAccount')}
            </Button>
          )}
        </CardContent>
      </Card>

      <Dialog open={remove !== null} onOpenChange={(open) => { if (!open && !busy) setRemove(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t(remove?.kind === 'account' ? 'studio.privacy.deleteAccount' : 'studio.privacy.confirmMemory')}
            </DialogTitle>
            <DialogDescription>
              {t(remove?.kind === 'account' ? 'studio.privacy.confirmAccount' : 'studio.privacy.memoryPermanent')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant='outline' disabled={busy} onClick={() => setRemove(null)}>{t('studio.account.cancel')}</Button>
            <Button variant='destructive' disabled={busy} onClick={() => void confirmRemoval()}>
              {t('studio.privacy.confirmDelete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
