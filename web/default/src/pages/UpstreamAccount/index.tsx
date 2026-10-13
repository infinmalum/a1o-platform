import React from 'react';
import { Card } from 'semantic-ui-react';
import { useTranslation } from 'react-i18next';
import UpstreamAccountsTable from '../../components/UpstreamAccountsTable';

const UpstreamAccount = () => {
  const { t } = useTranslation();

  return (
    <div className='dashboard-container'>
      <Card fluid className='chart-card'>
        <Card.Content>
          <Card.Header className='header'>
            {t('upstreamAccount.title')}
          </Card.Header>
          <UpstreamAccountsTable />
        </Card.Content>
      </Card>
    </div>
  );
};

export default UpstreamAccount;
