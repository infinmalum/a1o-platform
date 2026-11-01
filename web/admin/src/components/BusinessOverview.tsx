import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Card,
  Divider,
  Grid,
  Header,
  Label,
  Statistic,
  Table,
} from 'semantic-ui-react';
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
    <Card fluid className='chart-card' style={{ marginBottom: '1em' }}>
      <Card.Content>
        <Card.Header>
          {t('business.title')}
          {attention > 0 && (
            <Label color='orange' style={{ marginLeft: '0.75em' }}>
              {t('business.attention', { count: attention })}
            </Label>
          )}
        </Card.Header>
        <p style={{ color: '#999', fontSize: '0.85em', marginTop: '0.5em' }}>
          {t('business.costNotice')}
        </p>

        <Statistic.Group widths='four' size='small' style={{ marginTop: '1em' }}>
          <Statistic color='blue'>
            <Statistic.Value>
              {renderQuota(data.today.consumed_quota, t)}
            </Statistic.Value>
            <Statistic.Label>{t('business.todayConsumed')}</Statistic.Label>
          </Statistic>
          <Statistic color='green'>
            <Statistic.Value>
              {renderQuota(data.today.topup_quota, t)}
            </Statistic.Value>
            <Statistic.Label>{t('business.todayTopup')}</Statistic.Label>
          </Statistic>
          <Statistic>
            <Statistic.Value>{data.today.requests}</Statistic.Value>
            <Statistic.Label>{t('business.todayRequests')}</Statistic.Label>
          </Statistic>
          <Statistic>
            <Statistic.Value>{data.today.active_users}</Statistic.Value>
            <Statistic.Label>{t('business.todayActiveUsers')}</Statistic.Label>
          </Statistic>
        </Statistic.Group>

        <Divider />

        <Statistic.Group widths='four' size='mini'>
          <Statistic>
            <Statistic.Value>
              {renderQuota(data.week.consumed_quota, t)}
            </Statistic.Value>
            <Statistic.Label>{t('business.weekConsumed')}</Statistic.Label>
          </Statistic>
          <Statistic>
            <Statistic.Value>
              {renderQuota(data.week.topup_quota, t)}
            </Statistic.Value>
            <Statistic.Label>{t('business.weekTopup')}</Statistic.Label>
          </Statistic>
          <Statistic>
            <Statistic.Value>{data.week.requests}</Statistic.Value>
            <Statistic.Label>{t('business.weekRequests')}</Statistic.Label>
          </Statistic>
          <Statistic>
            <Statistic.Value>{data.total_users}</Statistic.Value>
            <Statistic.Label>{t('business.totalUsers')}</Statistic.Label>
          </Statistic>
        </Statistic.Group>

        <Divider />

        <Grid columns={2} stackable>
          <Grid.Column>
            <Header as='h4'>{t('business.upstream.title')}</Header>
            <Label.Group>
              <Label basic>
                {t('business.upstream.total')}
                <Label.Detail>{data.channels.total}</Label.Detail>
              </Label>
              <Label basic color='green'>
                {t('business.upstream.enabled')}
                <Label.Detail>{data.channels.enabled}</Label.Detail>
              </Label>
              <Label basic color='red'>
                {t('business.upstream.autoDisabled')}
                <Label.Detail>{data.channels.auto_disabled}</Label.Detail>
              </Label>
              <Label basic color='grey'>
                {t('business.upstream.manuallyDisabled')}
                <Label.Detail>{data.channels.manually_disabled}</Label.Detail>
              </Label>
              <Label basic color='orange'>
                {t('business.upstream.lowBalance')}
                <Label.Detail>{data.channels.low_balance}</Label.Detail>
              </Label>
              <Label basic>
                {t('business.upstream.balanceUnknown')}
                <Label.Detail>{data.channels.balance_unavailable}</Label.Detail>
              </Label>
            </Label.Group>
            <p style={{ color: '#999', fontSize: '0.85em' }}>
              {t('business.upstream.lowBalanceHint', {
                threshold: data.low_balance_threshold,
              })}
            </p>
            <Link to='/channel'>{t('business.upstream.manage')}</Link>
          </Grid.Column>
          <Grid.Column>
            <Header as='h4'>{t('business.topModels')}</Header>
            <Table compact basic='very'>
              <Table.Body>
                {data.top_models.map((model) => (
                  <Table.Row key={model.model_name}>
                    <Table.Cell>{model.model_name}</Table.Cell>
                    <Table.Cell textAlign='right'>
                      {renderQuota(model.quota, t)}
                    </Table.Cell>
                    <Table.Cell textAlign='right' style={{ color: '#999' }}>
                      {model.requests}
                    </Table.Cell>
                  </Table.Row>
                ))}
                {data.top_models.length === 0 && (
                  <Table.Row>
                    <Table.Cell style={{ color: '#999' }}>
                      {t('business.noUsageToday')}
                    </Table.Cell>
                  </Table.Row>
                )}
              </Table.Body>
            </Table>
          </Grid.Column>
        </Grid>
      </Card.Content>
    </Card>
  );
};

export default BusinessOverview;
