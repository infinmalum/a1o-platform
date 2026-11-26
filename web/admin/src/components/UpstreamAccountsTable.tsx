import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
} from 'antd';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
} from '../helpers';

import { ITEMS_PER_PAGE } from '../constants';
import UpstreamAccountOAuthWizard from './UpstreamAccountOAuthWizard';

function renderStatus(status, t) {
  switch (status) {
    case 1:
      return <Tag color='green'>{t('upstreamAccount.status.enabled')}</Tag>;
    case 2:
      return <Tag color='red'>{t('upstreamAccount.status.disabled')}</Tag>;
    case 3:
      return <Tag color='gold'>{t('upstreamAccount.status.reauthRequired')}</Tag>;
    default:
      return <Tag>{t('upstreamAccount.status.unknown')}</Tag>;
  }
}

const UpstreamAccountsTable = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const [accounts, setAccounts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizardAccountId, setWizardAccountId] = useState<number | undefined>(
    undefined
  );
  const [wizardAccountName, setWizardAccountName] = useState<string | undefined>(
    undefined
  );
  const [wizardProfile, setWizardProfile] = useState<string | undefined>(
    undefined
  );
  const [editing, setEditing] = useState(null);
  const [saving, setSaving] = useState(false);

  const loadAccounts = async (startIdx) => {
    const res = await API.get(`/api/upstream_account/?p=${startIdx}`);
    const { success, message, data } = res.data;
    if (success) {
      if (startIdx === 0) {
        setAccounts(data);
      } else {
        let newAccounts = [...accounts];
        newAccounts.push(...data);
        setAccounts(newAccounts);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (page: number) => {
    (async () => {
      if (page === Math.ceil(accounts.length / ITEMS_PER_PAGE) + 1) {
        await loadAccounts(page - 1);
      }
      setActivePage(page);
    })();
  };

  useEffect(() => {
    loadAccounts(0)
      .then()
      .catch((reason) => {
        showError(reason);
      });
  }, []);

  const openConnectWizard = () => {
    setWizardAccountId(undefined);
    setWizardAccountName(undefined);
    setWizardProfile(undefined);
    setWizardOpen(true);
  };

  const openReconnectWizard = (account) => {
    setWizardAccountId(account.id);
    setWizardAccountName(account.name);
    setWizardProfile(account.profile);
    setWizardOpen(true);
  };

  const onWizardCompleted = () => {
    setLoading(true);
    setActivePage(1);
    loadAccounts(0).then();
  };

  // The metadata update overwrites name and concurrency in one write, so the
  // UI always sends the current values alongside the changed field.
  const saveAccount = async (account, overrides, successMessage) => {
    const res = await API.put('/api/upstream_account/', {
      id: account.id,
      name: 'name' in overrides ? overrides.name : account.name,
      concurrency_limit:
        'concurrency_limit' in overrides
          ? overrides.concurrency_limit
          : account.concurrency_limit,
      status: 'status' in overrides ? overrides.status : undefined,
    });
    const { success, message } = res.data;
    if (success) {
      showSuccess(successMessage);
      let newAccounts = [...accounts];
      for (let i = 0; i < newAccounts.length; i++) {
        if (newAccounts[i].id === account.id) {
          newAccounts[i] = { ...newAccounts[i], ...overrides };
        }
      }
      setAccounts(newAccounts);
    } else {
      showError(message);
    }
    return success;
  };

  const toggleStatus = async (account, status) => {
    await saveAccount(
      account,
      { status },
      t('upstreamAccount.messages.updateSuccess')
    );
  };

  const deleteAccount = async (account) => {
    const res = await API.delete(`/api/upstream_account/${account.id}/`);
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('upstreamAccount.messages.deleteSuccess'));
      let newAccounts = [...accounts];
      for (let i = 0; i < newAccounts.length; i++) {
        if (newAccounts[i].id === account.id) {
          newAccounts[i].deleted = true;
        }
      }
      setAccounts(newAccounts);
    } else {
      showError(message);
    }
  };

  const openEditModal = (account) => {
    setEditing(account);
    form.setFieldsValue({
      name: account.name,
      concurrency_limit: account.concurrency_limit || 0,
    });
  };

  const submitEdit = async () => {
    const values = await form.validateFields();
    setSaving(true);
    const ok = await saveAccount(
      editing,
      { name: values.name.trim(), concurrency_limit: values.concurrency_limit },
      t('upstreamAccount.messages.updateSuccess')
    );
    setSaving(false);
    if (ok) {
      setEditing(null);
    }
  };

  const refresh = async () => {
    setLoading(true);
    setActivePage(1);
    await loadAccounts(0);
  };

  const columns = [
    {
      title: t('upstreamAccount.table.id'),
      dataIndex: 'id',
      width: 60,
    },
    {
      title: t('upstreamAccount.table.name'),
      dataIndex: 'name',
    },
    {
      title: t('upstreamAccount.table.profile'),
      key: 'profile',
      render: (_, account) => `${account.provider} / ${account.profile}`,
    },
    {
      title: t('upstreamAccount.table.status'),
      dataIndex: 'status',
      render: (status) => renderStatus(status, t),
    },
    {
      title: t('upstreamAccount.table.externalId'),
      dataIndex: 'external_id',
    },
    {
      title: t('upstreamAccount.table.credentialExpiry'),
      dataIndex: 'credential_expires_at',
      render: (value) =>
        value
          ? timestamp2string(value)
          : t('upstreamAccount.table.unknown'),
    },
    {
      title: t('upstreamAccount.table.concurrencyLimit'),
      dataIndex: 'concurrency_limit',
      render: (value) =>
        value > 0 ? value : t('upstreamAccount.table.unlimited'),
    },
    {
      title: t('upstreamAccount.table.createdTime'),
      dataIndex: 'created_time',
      render: (value) => timestamp2string(value),
    },
    {
      title: t('upstreamAccount.table.actions'),
      key: 'actions',
      render: (_, account) => (
        <Space size={4} wrap>
          {account.status === 3 ? (
            <Button
              size='small'
              type='primary'
              onClick={() => openReconnectWizard(account)}
            >
              {t('upstreamAccount.buttons.reconnect')}
            </Button>
          ) : (
            <Button size='small' onClick={() => openReconnectWizard(account)}>
              {t('upstreamAccount.buttons.reconnect')}
            </Button>
          )}
          {account.status === 2 ? (
            <Button size='small' onClick={() => toggleStatus(account, 1)}>
              {t('upstreamAccount.buttons.enable')}
            </Button>
          ) : (
            <Button size='small' onClick={() => toggleStatus(account, 2)}>
              {t('upstreamAccount.buttons.disable')}
            </Button>
          )}
          <Button size='small' onClick={() => openEditModal(account)}>
            {t('upstreamAccount.buttons.edit')}
          </Button>
          <Popconfirm
            title={t('upstreamAccount.buttons.confirmDelete')}
            onConfirm={() => deleteAccount(account)}
          >
            <Button size='small' danger>
              {t('upstreamAccount.buttons.delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const pageAccounts = accounts.filter((account) => !account.deleted);

  return (
    <>
      <Alert
        type='warning'
        showIcon
        message={t('upstreamAccount.disclaimerBanner')}
        style={{ marginBottom: 16 }}
      />
      <Space style={{ marginBottom: 16 }}>
        <Button type='primary' loading={loading} onClick={openConnectWizard}>
          {t('upstreamAccount.buttons.connect')}
        </Button>
        <Button onClick={refresh} loading={loading}>
          {t('upstreamAccount.buttons.refresh')}
        </Button>
      </Space>
      <Table
        size='small'
        columns={columns}
        dataSource={pageAccounts.slice(
          (activePage - 1) * ITEMS_PER_PAGE,
          activePage * ITEMS_PER_PAGE
        )}
        rowKey='id'
        pagination={{
          current: activePage,
          pageSize: ITEMS_PER_PAGE,
          total: pageAccounts.length,
          showSizeChanger: false,
          onChange: onPaginationChange,
        }}
      />

      <UpstreamAccountOAuthWizard
        open={wizardOpen}
        accountId={wizardAccountId}
        accountName={wizardAccountName}
        profile={wizardProfile}
        onClose={() => setWizardOpen(false)}
        onCompleted={onWizardCompleted}
      />

      <Modal
        open={editing !== null}
        onCancel={() => setEditing(null)}
        title={t('upstreamAccount.edit.title')}
        footer={[
          <Button key='cancel' onClick={() => setEditing(null)}>
            {t('upstreamAccount.edit.cancel')}
          </Button>,
          <Button key='submit' type='primary' loading={saving} onClick={submitEdit}>
            {t('upstreamAccount.edit.submit')}
          </Button>,
        ]}
      >
        <Form form={form} layout='vertical'>
          <Form.Item
            name='name'
            label={t('upstreamAccount.edit.name')}
            rules={[{ required: true }]}
          >
            <Input placeholder={t('upstreamAccount.edit.namePlaceholder')} />
          </Form.Item>
          <Form.Item name='concurrency_limit' label={t('upstreamAccount.edit.concurrencyLimit')}>
            <InputNumber
              min={0}
              style={{ width: '100%' }}
              placeholder={t('upstreamAccount.edit.concurrencyPlaceholder')}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default UpstreamAccountsTable;
