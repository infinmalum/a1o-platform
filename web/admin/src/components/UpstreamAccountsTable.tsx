import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Form,
  Header,
  Label,
  Modal,
  Pagination,
  Popup,
  Table,
} from 'semantic-ui-react';
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
      return (
        <Label basic color='green'>
          {t('upstreamAccount.status.enabled')}
        </Label>
      );
    case 2:
      return (
        <Label basic color='red'>
          {t('upstreamAccount.status.disabled')}
        </Label>
      );
    case 3:
      return (
        <Label basic color='yellow'>
          {t('upstreamAccount.status.reauthRequired')}
        </Label>
      );
    default:
      return (
        <Label basic color='black'>
          {t('upstreamAccount.status.unknown')}
        </Label>
      );
  }
}

const UpstreamAccountsTable = () => {
  const { t } = useTranslation();
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
  const [editing, setEditing] = useState(null);
  const [editName, setEditName] = useState('');
  const [editConcurrency, setEditConcurrency] = useState('');
  const [saving, setSaving] = useState(false);

  const loadAccounts = async (startIdx) => {
    const res = await API.get(`/api/upstream_account/?p=${startIdx}`);
    const { success, message, data } = res.data;
    if (success) {
      if (startIdx === 0) {
        setAccounts(data);
      } else {
        let newAccounts = accounts;
        newAccounts.push(...data);
        setAccounts(newAccounts);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (
    e,
    { activePage: rawPage }: { activePage?: number | string }
  ) => {
    const activePage = Number(rawPage);
    if (!Number.isFinite(activePage)) return;
    (async () => {
      if (activePage === Math.ceil(accounts.length / ITEMS_PER_PAGE) + 1) {
        await loadAccounts(activePage - 1);
      }
      setActivePage(activePage);
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
    setWizardOpen(true);
  };

  const openReconnectWizard = (account) => {
    setWizardAccountId(account.id);
    setWizardAccountName(account.name);
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
    setEditName(account.name);
    setEditConcurrency(String(account.concurrency_limit || 0));
  };

  const submitEdit = async () => {
    const concurrency = parseInt(editConcurrency, 10);
    if (!Number.isFinite(concurrency) || concurrency < 0) {
      showError(t('upstreamAccount.messages.invalidConcurrency'));
      return;
    }
    setSaving(true);
    const ok = await saveAccount(
      editing,
      { name: editName.trim(), concurrency_limit: concurrency },
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

  return (
    <>
      <Table basic={'very'} compact size='small'>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>{t('upstreamAccount.table.id')}</Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.name')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.profile')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.status')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.externalId')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.credentialExpiry')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.concurrencyLimit')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.createdTime')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('upstreamAccount.table.actions')}
            </Table.HeaderCell>
          </Table.Row>
        </Table.Header>

        <Table.Body>
          {accounts
            .slice(
              (activePage - 1) * ITEMS_PER_PAGE,
              activePage * ITEMS_PER_PAGE
            )
            .map((account) => {
              if (account.deleted) return <></>;
              return (
                <Table.Row key={account.id}>
                  <Table.Cell>{account.id}</Table.Cell>
                  <Table.Cell>{account.name}</Table.Cell>
                  <Table.Cell>
                    {account.provider} / {account.profile}
                  </Table.Cell>
                  <Table.Cell>{renderStatus(account.status, t)}</Table.Cell>
                  <Table.Cell>{account.external_id}</Table.Cell>
                  <Table.Cell>
                    {account.credential_expires_at
                      ? timestamp2string(account.credential_expires_at)
                      : t('upstreamAccount.table.unknown')}
                  </Table.Cell>
                  <Table.Cell>
                    {account.concurrency_limit > 0
                      ? account.concurrency_limit
                      : t('upstreamAccount.table.unlimited')}
                  </Table.Cell>
                  <Table.Cell>
                    {timestamp2string(account.created_time)}
                  </Table.Cell>
                  <Table.Cell>
                    <div>
                      <Button
                        size={'tiny'}
                        primary={account.status === 3}
                        onClick={() => openReconnectWizard(account)}
                      >
                        {t('upstreamAccount.buttons.reconnect')}
                      </Button>
                      {account.status === 2 && (
                        <Button
                          size={'tiny'}
                          onClick={() => toggleStatus(account, 1)}
                        >
                          {t('upstreamAccount.buttons.enable')}
                        </Button>
                      )}
                      {account.status !== 2 && (
                        <Button
                          size={'tiny'}
                          onClick={() => toggleStatus(account, 2)}
                        >
                          {t('upstreamAccount.buttons.disable')}
                        </Button>
                      )}
                      <Button
                        size={'tiny'}
                        onClick={() => openEditModal(account)}
                      >
                        {t('upstreamAccount.buttons.edit')}
                      </Button>
                      <Popup
                        trigger={
                          <Button size='tiny' negative>
                            {t('upstreamAccount.buttons.delete')}
                          </Button>
                        }
                        on='click'
                        flowing
                        hoverable
                      >
                        <Button
                          negative
                          onClick={() => {
                            deleteAccount(account);
                          }}
                        >
                          {t('upstreamAccount.buttons.confirmDelete')}
                        </Button>
                      </Popup>
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })}
        </Table.Body>

        <Table.Footer>
          <Table.Row>
            <Table.HeaderCell colSpan='9'>
              <Button
                size='small'
                primary
                loading={loading}
                onClick={openConnectWizard}
              >
                {t('upstreamAccount.buttons.connect')}
              </Button>
              <Button size='small' onClick={refresh} loading={loading}>
                {t('upstreamAccount.buttons.refresh')}
              </Button>
              <Pagination
                floated='right'
                activePage={activePage}
                onPageChange={onPaginationChange}
                size='small'
                siblingRange={1}
                totalPages={
                  Math.ceil(accounts.length / ITEMS_PER_PAGE) +
                  (accounts.length % ITEMS_PER_PAGE === 0 ? 1 : 0)
                }
              />
            </Table.HeaderCell>
          </Table.Row>
        </Table.Footer>
      </Table>

      <UpstreamAccountOAuthWizard
        open={wizardOpen}
        accountId={wizardAccountId}
        accountName={wizardAccountName}
        onClose={() => setWizardOpen(false)}
        onCompleted={onWizardCompleted}
      />

      <Modal
        open={editing !== null}
        onClose={() => setEditing(null)}
        size='small'
        closeIcon
      >
        <Header>{t('upstreamAccount.edit.title')}</Header>
        <Modal.Content>
          <Form>
            <Form.Input
              label={t('upstreamAccount.edit.name')}
              placeholder={t('upstreamAccount.edit.namePlaceholder')}
              value={editName}
              onChange={(e, { value }) => setEditName(value)}
            />
            <Form.Input
              label={t('upstreamAccount.edit.concurrencyLimit')}
              placeholder={t('upstreamAccount.edit.concurrencyPlaceholder')}
              value={editConcurrency}
              onChange={(e, { value }) => setEditConcurrency(value)}
            />
          </Form>
        </Modal.Content>
        <Modal.Actions>
          <Button onClick={() => setEditing(null)}>
            {t('upstreamAccount.edit.cancel')}
          </Button>
          <Button primary loading={saving} disabled={saving} onClick={submitEdit}>
            {t('upstreamAccount.edit.submit')}
          </Button>
        </Modal.Actions>
      </Modal>
    </>
  );
};

export default UpstreamAccountsTable;
