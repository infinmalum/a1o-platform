import React from 'react';
import { useTranslation } from 'react-i18next';
import { Card, Tabs } from 'antd';
import SystemSetting from '../../components/SystemSetting';
import { isRoot } from '../../helpers';
import OtherSetting from '../../components/OtherSetting';
import PersonalSetting from '../../components/PersonalSetting';
import OperationSetting from '../../components/OperationSetting';
import ChatAdminSetting from '../../components/ChatAdminSetting';
import StudioLanguageSetting from '../../components/StudioLanguageSetting';

const Setting = () => {
  const { t } = useTranslation();

  const items = [
    {
      key: 'personal',
      label: t('setting.tabs.personal'),
      children: <PersonalSetting />,
    },
  ];

  if (isRoot()) {
    items.push(
      {
        key: 'operation',
        label: t('setting.tabs.operation'),
        children: <OperationSetting />,
      },
      {
        key: 'chat',
        label: t('setting.tabs.chat'),
        children: <ChatAdminSetting />,
      },
      {
        key: 'system',
        label: t('setting.tabs.system'),
        children: <SystemSetting />,
      },
      {
        key: 'studio_languages',
        label: t('setting.tabs.studio_languages'),
        children: <StudioLanguageSetting />,
      },
      {
        key: 'other',
        label: t('setting.tabs.other'),
        children: <OtherSetting />,
      }
    );
  }

  return (
    <Card title={t('setting.title')}>
      <Tabs items={items} destroyInactiveTabPane />
    </Card>
  );
};

export default Setting;
