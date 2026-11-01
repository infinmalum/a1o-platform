const DEFAULT_QUOTA_PER_UNIT = 500000;

export function getQuotaPerUnitRaw() {
  const perUnit = parseFloat(localStorage.getItem('quota_per_unit') || '');
  return Number.isFinite(perUnit) && perUnit > 0 ? perUnit : DEFAULT_QUOTA_PER_UNIT;
}

export function renderNumber(num: number) {
  if (num >= 1000000000) {
    return (num / 1000000000).toFixed(1) + 'B';
  } else if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M';
  } else if (num >= 10000) {
    return (num / 1000).toFixed(1) + 'k';
  }
  return String(num);
}

// The backend stores integer quota with quota_per_unit units per $1; the
// status payload cached at startup decides how balances display.
export function renderQuota(
  quota: number,
  t: (key: string, opts?: Record<string, unknown>) => string,
  precision = 2
) {
  const displayInCurrency = localStorage.getItem('display_in_currency') === 'true';
  if (displayInCurrency) {
    const amount = (quota / getQuotaPerUnitRaw()).toFixed(precision);
    return t('common.quota.display_short', { amount });
  }
  return renderNumber(quota);
}
