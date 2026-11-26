import React from 'react';
import { Card } from 'antd';
import ChannelsTable from '../../components/ChannelsTable';
import { useTranslation } from 'react-i18next';

const Channel = () => {
  const { t } = useTranslation();

  return (
    <Card title={t('channel.title')}>
      <ChannelsTable />
    </Card>
  );
};

export default Channel;
