import React from 'react';
import { Card } from 'antd';
import { useTranslation } from 'react-i18next';
import LogsTable from '../../components/LogsTable';

const Log = () => {
  const { t } = useTranslation();

  return (
    <Card title={t('log.title')}>
      <LogsTable />
    </Card>
  );
};

export default Log;
