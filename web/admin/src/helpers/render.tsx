import { Tag } from 'antd';
import { getChannelOption } from './helper';
import React from 'react';

export function renderText(text, limit) {
  if (text.length > limit) {
    return text.slice(0, limit - 3) + '...';
  }
  return text;
}

export function renderGroup(group) {
  if (group === '') {
    return <Tag>default</Tag>;
  }
  let groups = group.split(',');
  groups.sort();
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '2px',
        rowGap: '6px',
      }}
    >
      {groups.map((group) => {
        if (group === 'vip' || group === 'pro') {
          return <Tag color='gold'>{group}</Tag>;
        } else if (group === 'svip' || group === 'premium') {
          return <Tag color='red'>{group}</Tag>;
        }
        return <Tag>{group}</Tag>;
      })}
    </div>
  );
}

export function renderNumber(num) {
  if (num >= 1000000000) {
    return (num / 1000000000).toFixed(1) + 'B';
  } else if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M';
  } else if (num >= 10000) {
    return (num / 1000).toFixed(1) + 'k';
  } else {
    return num;
  }
}

export function renderQuota(quota, t, precision = 2) {
  const displayInCurrency =
    localStorage.getItem('display_in_currency') === 'true';
  const quotaPerUnit = parseFloat(
    localStorage.getItem('quota_per_unit') || '1'
  );

  if (displayInCurrency) {
    const amount = (quota / quotaPerUnit).toFixed(precision);
    return t('common.quota.display_short', { amount });
  }

  return renderNumber(quota);
}


// Quota/amount converters for money inputs. The backend stores integer quota
// with config.QuotaPerUnit quota per $1 (500000 by default), so decimal
// amounts like $2.56 must be converted to a whole number before submit.
const DEFAULT_QUOTA_PER_UNIT = 500000;

export function getQuotaPerUnit() {
  const perUnit = parseFloat(localStorage.getItem('quota_per_unit') || '');
  return Number.isFinite(perUnit) && perUnit > 0
    ? perUnit
    : DEFAULT_QUOTA_PER_UNIT;
}

export function quotaToAmount(quota) {
  return (Number(quota) || 0) / getQuotaPerUnit();
}

export function amountToQuota(amount) {
  return Math.round((Number(amount) || 0) * getQuotaPerUnit());
}

export function formatAmount(amount) {
  return String(Math.round((Number(amount) || 0) * 100) / 100);
}

const colors = [
  'red',
  'orange',
  'gold',
  'lime',
  'green',
  'cyan',
  'blue',
  'geekblue',
  'purple',
  'magenta',
  'volcano',
  'default',
] as const;

export function renderColorLabel(text) {
  let hash = 0;
  for (let i = 0; i < text.length; i++) {
    hash = text.charCodeAt(i) + ((hash << 5) - hash);
  }
  let index = Math.abs(hash % colors.length);
  return <Tag color={colors[index]}>{text}</Tag>;
}

export function renderChannelTip(channelId) {
  let channel = getChannelOption(channelId);
  if (channel === undefined || channel.tip === undefined) {
    return <></>;
  }
  return (
    <div className='channel-tip'>{channel.tip}</div>
  );
}
