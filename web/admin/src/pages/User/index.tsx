import React from 'react';
import { Card } from 'antd';
import { useTranslation } from 'react-i18next';
import UsersTable from '../../components/UsersTable';

const User = () => {
  const { t } = useTranslation();

  return (
    <Card title={t('user.title')}>
      <UsersTable />
    </Card>
  );
};

export default User;
