import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card, Divider, Space, Tag, Typography } from 'antd';
import { Link } from 'react-router-dom';

import { API, isAdmin } from '../helpers';
import { renderQuota } from '../helpers/render';

interface WindowStats {
  consumed_quota: number;
  topup_quota: number;
  requests: number;
  active_users: number;
}

interface ChannelHealth {
  total: number;
  enabled: number;
  manually_disabled: number;
  auto_disabled: number;
  low_balance: number;
  balance_unavailable: number;
}

interface BusinessData {
  today: WindowStats;
  week: WindowStats;
  top_models: { model_name: string; quota: number; requests: number }[];
  channels: ChannelHealth;
  total_users: number;
  low_balance_threshold: number;
}

const statValueStyle = { fontSize: '1.6em', fontWeight: 600, lineHeight: 1.3 };
const statLabelStyle = { color: '#999', fontSize: '0.85em' };

// BusinessOverview is the administrator's first screen: what the deployment
// charged, what users paid in, who used it, and which upstreams need attention.
// Upstream cost and margin are absent because the gateway stores no cost per
// model, and a made-up margin would be worse than none.
const BusinessOverview = () => {
  const { t } = useTranslation();
  const [data, setData] = useState<BusinessData | null>(null);

  useEffect(() => {
    if (!isAdmin()) {
      return;
    }
    const load = async () => {
      try {
        const res = await API.get('/api/dashboard/business');
        const { success, data } = res.data;
        if (success) {
          setData(data);
        }
      } catch {
        // the interceptor toasts the error
      }
    };
    load().then();
  }, []);

  if (!data) {
    return <></>;
  }

  const attention =
    data.channels.auto_disabled +
    data.channels.manually_disabled +
    data.channels.low_balance;
  const topModels = data.top_models || [];

  return (
    <Card className='chart-card' style={{ marginBottom: '1em' }}>
      <Typography.Title level={4}>
        {t('business.title')}
        {attention > 0 && (
          <Tag color='orange' style={{ marginLeft: '0.75em', verticalAlign: 'middle' }}>
            {t('business.attention', { count: attention })}
          </Tag>
        )}
      </Typography.Title>
      <Typography.Paragraph type='secondary' style={{ fontSize: '0.85em' }}>
        {t('business.costNotice')}
      </Typography.Paragraph>

      <Space size={48} wrap style={{ display: 'flex', marginTop: 8 }}>
        <div>
          <div style={{ ...statValueStyle, color: '#1677ff' }}>
            {renderQuota(data.today.consumed_quota, t)}
          </div>
          <div style={statLabelStyle}>{t('business.todayConsumed')}</div>
        </div>
        <div>
          <div style={{ ...statValueStyle, color: '#52c41a' }}>
            {renderQuota(data.today.topup_quota, t)}
          </div>
          <div style={statLabelStyle}>{t('business.todayTopup')}</div>
        </div>
        <div>
          <div style={statValueStyle}>{data.today.requests}</div>
          <div style={statLabelStyle}>{t('business.todayRequests')}</div>
        </div>
        <div>
          <div style={statValueStyle}>{data.today.active_users}</div>
          <div style={statLabelStyle}>{t('business.todayActiveUsers')}</div>
        </div>
      </Space>

      <Divider />

      <Space size={48} wrap style={{ display: 'flex' }}>
        <div>
          <div style={{ ...statValueStyle, fontSize: '1.3em' }}>
            {renderQuota(data.week.consumed_quota, t)}
          </div>
          <div style={statLabelStyle}>{t('business.weekConsumed')}</div>
        </div>
        <div>
          <div style={{ ...statValueStyle, fontSize: '1.3em' }}>
            {renderQuota(data.week.topup_quota, t)}
          </div>
          <div style={statLabelStyle}>{t('business.weekTopup')}</div>
        </div>
        <div>
          <div style={{ ...statValueStyle, fontSize: '1.3em' }}>
            {data.week.requests}
          </div>
          <div style={statLabelStyle}>{t('business.weekRequests')}</div>
        </div>
        <div>
          <div style={{ ...statValueStyle, fontSize: '1.3em' }}>
            {data.total_users}
          </div>
          <div style={statLabelStyle}>{t('business.totalUsers')}</div>
        </div>
      </Space>

      <Divider />

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: 24 }}>
        <div>
          <Typography.Title level={5}>{t('business.upstream.title')}</Typography.Title>
          <Space size={8} wrap style={{ marginBottom: 8 }}>
            <Tag>
              {t('business.upstream.total')} {data.channels.total}
            </Tag>
            <Tag color='green'>
              {t('business.upstream.enabled')} {data.channels.enabled}
            </Tag>
            <Tag color='red'>
              {t('business.upstream.autoDisabled')} {data.channels.auto_disabled}
            </Tag>
            <Tag>
              {t('business.upstream.manuallyDisabled')} {data.channels.manually_disabled}
            </Tag>
            <Tag color='orange'>
              {t('business.upstream.lowBalance')} {data.channels.low_balance}
            </Tag>
            <Tag>
              {t('business.upstream.balanceUnknown')} {data.channels.balance_unavailable}
            </Tag>
          </Space>
          <Typography.Paragraph type='secondary' style={{ fontSize: '0.85em' }}>
            {t('business.upstream.lowBalanceHint', {
              threshold: data.low_balance_threshold,
            })}
          </Typography.Paragraph>
          <Link to='/channel'>{t('business.upstream.manage')}</Link>
        </div>
        <div>
          <Typography.Title level={5}>{t('business.topModels')}</Typography.Title>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <tbody>
              {topModels.map((model) => (
                <tr key={model.model_name}>
                  <td style={{ padding: '4px 0' }}>{model.model_name}</td>
                  <td style={{ padding: '4px 0', textAlign: 'right' }}>
                    {renderQuota(model.quota, t)}
                  </td>
                  <td
                    style={{
                      padding: '4px 0',
                      textAlign: 'right',
                      color: '#999',
                    }}
                  >
                    {model.requests}
                  </td>
                </tr>
              ))}
              {topModels.length === 0 && (
                <tr>
                  <td style={{ color: '#999' }}>{t('business.noUsageToday')}</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </Card>
  );
};

export default BusinessOverview;
