import React from 'react';
import { Card } from 'antd';
import { useTranslation } from 'react-i18next';
import RedemptionsTable from '../../components/RedemptionsTable';

const Redemption = () => {
  const { t } = useTranslation();

  return (
    <Card title={t('redemption.title')}>
      <RedemptionsTable />
    </Card>
  );
};

export default Redemption;
