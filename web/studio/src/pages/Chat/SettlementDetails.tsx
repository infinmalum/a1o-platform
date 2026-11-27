import { useTranslation } from 'react-i18next';
import { renderQuota } from '@/helpers/render';
import type { SettledUsage } from './settlementApi';

export default function SettlementDetails({ usage }: { usage: SettledUsage }) {
  const { t } = useTranslation();
  return (
    <>
      {usage.usage_state === 'missing' ? (
        <span>{t('playground.tokensUnavailable')}</span>
      ) : (
        <span>{t(usage.usage_state === 'partial' ? 'playground.partialTokens' : 'playground.tokens', {
          input: usage.prompt_tokens, output: usage.completion_tokens,
        })}</span>
      )}
      <span>{t(usage.charge_basis === 'reservation' ? 'playground.reservedCost' : 'playground.cost', {
        amount: renderQuota(usage.quota, t, 6),
      })}</span>
    </>
  );
}
