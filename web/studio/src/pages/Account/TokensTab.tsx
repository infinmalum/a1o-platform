import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Copy, KeyRound, Loader2, Plus } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { formatStudioDate, getQuotaPerUnitRaw, renderQuota } from '@/helpers/render';
import { showError, showSuccess } from '@/helpers/utils';
import {
  accountApi,
  TOKEN_STATUS_DISABLED,
  TOKEN_STATUS_ENABLED,
  TOKEN_STATUS_EXPIRED,
  TOKEN_STATUS_EXHAUSTED,
  displayKey,
  type TokenPayload,
  type TokenView,
} from './accountApi';

const NEVER_EXPIRE = -1;
const DAY_SECONDS = 86400;

interface TokenFormState {
  id: number;
  name: string;
  neverExpires: boolean;
  expiresInDays: number;
  unlimited: boolean;
  quotaUsd: string;
  models: string;
}

const emptyForm = (): TokenFormState => ({
  id: 0,
  name: '',
  neverExpires: true,
  expiresInDays: 30,
  unlimited: true,
  quotaUsd: '5',
  models: '',
});

const statusVariant = (status: number): 'default' | 'secondary' | 'destructive' | 'outline' => {
  switch (status) {
    case TOKEN_STATUS_ENABLED:
      return 'default';
    case TOKEN_STATUS_DISABLED:
      return 'outline';
    case TOKEN_STATUS_EXPIRED:
    case TOKEN_STATUS_EXHAUSTED:
      return 'destructive';
    default:
      return 'secondary';
  }
};

const formatDate = (ts: number, t: (key: string) => string) => {
  if (ts === NEVER_EXPIRE || ts <= 0) return t('studio.account.tokens.never');
  return formatStudioDate(ts, true);
};

// API keys tab: the token CRUD the /v1 gateway authenticates against. The
// endpoints are owner-scoped server-side; the key itself only leaves the
// server once at creation, so copies happen from the listed row value.
const TokensTab: React.FC = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState<TokenView[]>([]);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<TokenFormState>(emptyForm());
  const [saving, setSaving] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<TokenView | null>(null);
  const [copiedId, setCopiedId] = useState(0);

  const reload = useCallback(async () => {
    try {
      setTokens(await accountApi.listTokens());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const openCreate = () => {
    setForm(emptyForm());
    setEditing(false);
    setDialogOpen(true);
  };

  const openEdit = (token: TokenView) => {
    setForm({
      id: token.id,
      name: token.name,
      neverExpires: token.expired_time === NEVER_EXPIRE,
      expiresInDays: Math.max(
        1,
        Math.ceil((token.expired_time - Date.now() / 1000) / DAY_SECONDS) || 1
      ),
      unlimited: token.unlimited_quota,
      quotaUsd: (token.remain_quota / getQuotaPerUnitRaw()).toFixed(2),
      models: token.models ?? '',
    });
    setEditing(true);
    setDialogOpen(true);
  };

  const submit = async () => {
    if (!form.name.trim()) {
      showError(t('studio.account.tokens.nameRequired'));
      return;
    }
    const payload: TokenPayload = {
      name: form.name.trim(),
      expired_time: form.neverExpires
        ? NEVER_EXPIRE
        : Math.floor(Date.now() / 1000) + form.expiresInDays * DAY_SECONDS,
      remain_quota: form.unlimited ? 0 : Math.round(parseFloat(form.quotaUsd || '0') * getQuotaPerUnitRaw()),
      unlimited_quota: form.unlimited,
      models: form.models.trim() ? form.models.trim() : null,
    };
    setSaving(true);
    try {
      if (editing) {
        await accountApi.updateToken({ ...payload, id: form.id });
        showSuccess(t('studio.account.tokens.updateSuccess'));
      } else {
        await accountApi.createToken(payload);
        showSuccess(t('studio.account.tokens.createSuccess'));
      }
      setDialogOpen(false);
      await reload();
    } catch {
      // error toast already surfaced by the request helper
    } finally {
      setSaving(false);
    }
  };

  const toggleStatus = async (token: TokenView) => {
    const next = token.status === TOKEN_STATUS_ENABLED ? TOKEN_STATUS_DISABLED : TOKEN_STATUS_ENABLED;
    await accountApi.setTokenStatus({ id: token.id, status: next });
    await reload();
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    await accountApi.deleteToken(deleteTarget.id);
    setDeleteTarget(null);
    await reload();
  };

  const copyKey = async (token: TokenView) => {
    await navigator.clipboard.writeText(displayKey(token.key));
    setCopiedId(token.id);
    window.setTimeout(() => setCopiedId((prev) => (prev === token.id ? 0 : prev)), 1500);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center justify-between'>
          <span className='flex items-center gap-2'>
            <KeyRound className='size-4' />
            {t('studio.account.tokens.title')}
          </span>
          <Button size='sm' onClick={openCreate}>
            <Plus className='size-4' />
            {t('studio.account.tokens.create')}
          </Button>
        </CardTitle>
        <CardDescription>{t('studio.account.tokens.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className='text-muted-foreground flex items-center justify-center py-10'>
            <Loader2 className='size-4 animate-spin' />
          </div>
        ) : tokens.length === 0 ? (
          <p className='text-muted-foreground py-10 text-center text-sm'>
            {t('studio.account.tokens.empty')}
          </p>
        ) : (
          <div className='overflow-x-auto'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='text-muted-foreground border-b text-start'>
                  <th className='py-2 pe-3 font-medium'>{t('studio.account.tokens.name')}</th>
                  <th className='py-2 pe-3 font-medium'>Key</th>
                  <th className='py-2 pe-3 font-medium'>{t('studio.account.tokens.status')}</th>
                  <th className='py-2 pe-3 font-medium'>{t('studio.account.tokens.quota')}</th>
                  <th className='py-2 pe-3 font-medium'>{t('studio.account.tokens.expiry')}</th>
                  <th className='py-2 font-medium'>{t('studio.account.tokens.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((token) => (
                  <tr key={token.id} className='border-b last:border-0'>
                    <td className='py-2 pe-3 font-medium'>{token.name}</td>
                    <td className='py-2 pe-3'>
                      <button
                        type='button'
                        onClick={() => void copyKey(token)}
                        className='hover:bg-accent inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-xs'
                        title={t('studio.account.tokens.copy')}
                      >
                        {displayKey(token.key).slice(0, 12)}…
                        {copiedId === token.id ? (
                          <Check className='text-muted-foreground size-3' />
                        ) : (
                          <Copy className='text-muted-foreground size-3' />
                        )}
                      </button>
                    </td>
                    <td className='py-2 pe-3'>
                      <Badge variant={statusVariant(token.status)}>
                        {t(
                          token.status === TOKEN_STATUS_ENABLED
                            ? 'studio.account.tokens.statusEnabled'
                            : token.status === TOKEN_STATUS_DISABLED
                              ? 'studio.account.tokens.statusDisabled'
                              : token.status === TOKEN_STATUS_EXPIRED
                                ? 'studio.account.tokens.statusExpired'
                                : 'studio.account.tokens.statusExhausted'
                        )}
                      </Badge>
                    </td>
                    <td className='py-2 pe-3'>
                      {token.unlimited_quota
                        ? t('studio.account.tokens.unlimited')
                        : `${t('studio.account.tokens.remaining')} ${renderQuota(token.remain_quota, t)} · ${t('studio.account.tokens.used')} ${renderQuota(token.used_quota, t)}`}
                    </td>
                    <td className='text-muted-foreground py-2 pe-3'>
                      {formatDate(token.expired_time, t)}
                    </td>
                    <td className='py-2'>
                      <div className='flex flex-wrap items-center gap-1'>
                        <Button variant='ghost' size='sm' onClick={() => void toggleStatus(token)}>
                          {token.status === TOKEN_STATUS_ENABLED
                            ? t('studio.account.tokens.disable')
                            : t('studio.account.tokens.enable')}
                        </Button>
                        <Button variant='ghost' size='sm' onClick={() => openEdit(token)}>
                          {t('studio.account.tokens.edit')}
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          className='text-destructive'
                          onClick={() => setDeleteTarget(token)}
                        >
                          {t('studio.account.tokens.delete')}
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing ? t('studio.account.tokens.editTitle') : t('studio.account.tokens.createTitle')}
            </DialogTitle>
            <DialogDescription>{t('studio.account.tokens.dialogDescription')}</DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-1.5'>
              <Label htmlFor='token-name'>{t('studio.account.tokens.name')}</Label>
              <Input
                id='token-name'
                value={form.name}
                maxLength={50}
                placeholder={t('studio.account.tokens.namePlaceholder')}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </div>
            <div className='flex items-center justify-between'>
              <Label htmlFor='token-unlimited'>{t('studio.account.tokens.quotaUnlimited')}</Label>
              <Switch
                id='token-unlimited'
                checked={form.unlimited}
                onCheckedChange={(checked) => setForm({ ...form, unlimited: checked })}
              />
            </div>
            {!form.unlimited && (
              <div className='space-y-1.5'>
                <Label htmlFor='token-quota'>{t('studio.account.tokens.quotaLimit')}</Label>
                <Input
                  id='token-quota'
                  type='number'
                  min='0'
                  step='0.01'
                  value={form.quotaUsd}
                  onChange={(e) => setForm({ ...form, quotaUsd: e.target.value })}
                />
                <p className='text-muted-foreground text-xs'>
                  ≈ {renderQuota(Math.round(parseFloat(form.quotaUsd || '0') * getQuotaPerUnitRaw()), t)}
                </p>
              </div>
            )}
            <div className='flex items-center justify-between'>
              <Label htmlFor='token-never'>{t('studio.account.tokens.neverExpires')}</Label>
              <Switch
                id='token-never'
                checked={form.neverExpires}
                onCheckedChange={(checked) => setForm({ ...form, neverExpires: checked })}
              />
            </div>
            {!form.neverExpires && (
              <div className='space-y-1.5'>
                <Label htmlFor='token-days'>{t('studio.account.tokens.expiresInDays')}</Label>
                <Input
                  id='token-days'
                  type='number'
                  min='1'
                  value={form.expiresInDays}
                  onChange={(e) => setForm({ ...form, expiresInDays: Math.max(1, parseInt(e.target.value || '1', 10)) })}
                />
              </div>
            )}
            <div className='space-y-1.5'>
              <Label htmlFor='token-models'>{t('studio.account.tokens.models')}</Label>
              <Input
                id='token-models'
                value={form.models}
                placeholder={t('studio.account.tokens.modelsPlaceholder')}
                onChange={(e) => setForm({ ...form, models: e.target.value })}
              />
              <p className='text-muted-foreground text-xs'>{t('studio.account.tokens.modelsHint')}</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDialogOpen(false)}>
              {t('studio.account.cancel')}
            </Button>
            <Button onClick={() => void submit()} disabled={saving}>
              {saving && <Loader2 className='size-4 animate-spin' />}
              {t('studio.account.save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('studio.account.tokens.deleteTitle')}</DialogTitle>
            <DialogDescription>
              {t('studio.account.tokens.deleteDescription', { name: deleteTarget?.name ?? '' })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDeleteTarget(null)}>
              {t('studio.account.cancel')}
            </Button>
            <Button variant='destructive' onClick={() => void confirmDelete()}>
              {t('studio.account.tokens.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
};

export default TokensTab;
