import React from 'react';
import { Card } from 'antd';
import { useTranslation } from 'react-i18next';
import UpstreamAccountsTable from '../../components/UpstreamAccountsTable';

const UpstreamAccount = () => {
  const { t } = useTranslation();

  return (
    <Card title={t('upstreamAccount.title')}>
      <UpstreamAccountsTable />
    </Card>
  );
};

export default UpstreamAccount;
