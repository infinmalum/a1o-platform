import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { renderQuota } from '@/helpers/render';
import { studioRequest as request } from '@/helpers/studioRequest';
import { accountApi, type UserSelf } from './accountApi';

type Provider = 'stripe' | 'nowpayments' | 'epay' | 'airwallex' | 'gmpay';
type Package = { id: string; name: string; amount: number; cny_amount?: number; quota: number };
type CryptoCurrency = { code: string; label: string };
type Catalog = { packages: Package[]; providers: Provider[]; nowpayments_currencies: CryptoCurrency[] };
type Order = {
  id: string;
  package_name: string;
  provider: Provider;
  amount: number;
  currency: string;
  quota: number;
  checkout_url?: string;
  credited_at: number;
  state?: string;
  pay_currency?: string;
  pay_currency_label?: string;
};

const providerNames: Record<Provider, string> = {
  stripe: 'Stripe',
  nowpayments: 'NOWPayments',
  epay: '易支付 · EPay',
  airwallex: 'Airwallex',
  gmpay: 'GMPay',
};

function packagePrice(pack: Package, provider: Provider) {
  return provider === 'epay' ? (pack.cny_amount ?? 0) : pack.amount;
}

export default function PaymentPackages({ onSelfUpdated }: { onSelfUpdated: (self: UserSelf) => void }) {
  const { t, i18n } = useTranslation();
  const [catalog, setCatalog] = useState<Catalog>();
  const [orders, setOrders] = useState<Order[]>([]);
  const [loadFailed, setLoadFailed] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [selectedCurrency, setSelectedCurrency] = useState('');
  const currencies = catalog?.nowpayments_currencies ?? [];
  const currency = currencies.find(({ code }) => code === selectedCurrency) ?? currencies[0];
  const returned = new URLSearchParams(window.location.search).get('payment') === 'return';
  const formatPrice = (amount: number, currency: string) =>
    new Intl.NumberFormat(i18n.resolvedLanguage, { style: 'currency', currency }).format(amount / 100);
  const purchasable = catalog?.packages.filter((pack) =>
    catalog.providers.some((provider) => packagePrice(pack, provider) > 0),
  ) ?? [];

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const [nextCatalog, nextOrders] = await Promise.all([
        request<Catalog>('/api/payments/packages', { signal }, false),
        request<Order[]>('/api/payments/orders', { signal }, false),
      ]);
      if (signal?.aborted) return;
      setCatalog(nextCatalog);
      setOrders(nextOrders);
      setLoadFailed(false);
    } catch {
      if (!signal?.aborted) setLoadFailed(true);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    if (returned) {
      void accountApi.getSelf().then((self) => {
        if (!controller.signal.aborted) onSelfUpdated(self);
      }).catch(() => undefined);
    }
    return () => controller.abort();
  }, [load, onSelfUpdated, returned]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([load(), accountApi.getSelf().then(onSelfUpdated).catch(() => undefined)]);
    } finally {
      setRefreshing(false);
    }
  };

  const buy = async (pack: Package, provider: Provider) => {
    if (busy) return;
    setBusy(pack.id + ':' + provider);
    try {
      const order = await request<Order>('/api/payments/orders', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          package_id: pack.id,
          provider,
          pay_currency: provider === 'nowpayments' ? currency?.code : undefined,
        }),
      });
      if (order.checkout_url) window.location.assign(order.checkout_url);
    } catch {
      // The request helper displays API errors.
    } finally {
      setBusy(null);
    }
  };

  if (loadFailed && !catalog) {
    return <Button variant='outline' onClick={() => void refresh()} disabled={refreshing}>{t('studio.account.payment.retry')}</Button>;
  }
  if (!catalog || (!purchasable.length && !orders.length)) return null;

  return (
    <Card>
      <CardHeader><CardTitle>{t('studio.account.payment.title')}</CardTitle></CardHeader>
      <CardContent className='space-y-4'>
        {returned && <p className='text-sm text-muted-foreground' role='status'>{t('studio.account.payment.returned')}</p>}
        {purchasable.some((pack) => pack.amount > 0) && currencies.length > 1 && (
          <label className='flex w-fit flex-col gap-1.5 text-sm font-medium'>
            {t('studio.account.payment.cryptoCurrency')}
            <select
              className='rounded-md border bg-background px-3 py-2 text-foreground'
              disabled={busy !== null}
              value={currency?.code ?? ''}
              onChange={(event) => setSelectedCurrency(event.target.value)}
            >
              {currencies.map(({ code, label }) => <option key={code} value={code}>{label}</option>)}
            </select>
          </label>
        )}
        {purchasable.length > 0 && (
          <ul className='grid gap-3 sm:grid-cols-2'>
            {purchasable.map((pack) => (
              <li key={pack.id} className='space-y-2 rounded-md border p-3'>
                <h3 className='font-medium'>{pack.name}</h3>
                <p className='text-sm'>{renderQuota(pack.quota, t)}</p>
                <div className='flex flex-wrap gap-2'>
                  {catalog.providers.filter((provider) => packagePrice(pack, provider) > 0).map((provider) => (
                    <Button key={provider} size='sm' variant='outline'
                      disabled={busy !== null || (provider === 'nowpayments' && !currency)}
                      onClick={() => void buy(pack, provider)}>
                      {busy === pack.id + ':' + provider ? t('studio.account.payment.opening') : providerNames[provider]}
                      {busy !== pack.id + ':' + provider && (
                        <bdi>· {formatPrice(packagePrice(pack, provider), provider === 'epay' ? 'CNY' : 'USD')}</bdi>
                      )}
                      {provider === 'nowpayments' && busy !== pack.id + ':' + provider && currency && (
                        <span dir='ltr'>· {currency.label}</span>
                      )}
                    </Button>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        )}
        {!!orders.length && (
          <div className='space-y-2'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <h3 className='font-medium'>{t('studio.account.payment.orders')}</h3>
              <Button size='sm' variant='outline' disabled={refreshing} onClick={() => void refresh()}>
                {t('studio.account.payment.refresh')}
              </Button>
            </div>
            {loadFailed && <p role='alert' className='text-sm'>{t('studio.account.payment.retry')}</p>}
            <ul className='divide-y'>
              {orders.map((order) => (
                <li key={order.id} className='flex flex-wrap justify-between gap-2 py-2 text-sm'>
                  <div>
                    <div>
                      {order.package_name} · {formatPrice(order.amount, order.currency)} · {providerNames[order.provider]}
                      {order.pay_currency && <bdi> · {order.pay_currency_label ?? order.pay_currency.toUpperCase()}</bdi>}
                    </div>
                    <code className='text-xs text-muted-foreground' dir='ltr'>{order.id}</code>
                  </div>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span>{t('studio.account.payment.' + (order.credited_at ? 'credited' : order.state === 'closed' ? 'closed' : order.state === 'review' ? 'review' : 'pending'))}</span>
                    {order.checkout_url && !order.credited_at && order.state !== 'closed' && (
                      <Button asChild size='sm' variant='outline'>
                        <a href={order.checkout_url}>{t('studio.account.payment.continue')}</a>
                      </Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
