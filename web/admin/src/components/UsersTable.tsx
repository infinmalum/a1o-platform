import React, { useEffect, useState } from 'react';
import {
  Button,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import { API, showError, showSuccess } from '../helpers';
import { useTranslation } from 'react-i18next';

import { ITEMS_PER_PAGE } from '../constants';
import QuotaInput from './QuotaInput';
import {
  renderGroup,
  renderNumber,
  renderQuota,
  renderText,
} from '../helpers/render';

function renderRole(role, t) {
  switch (role) {
    case 1:
      return <Tag>{t('user.table.role_types.normal')}</Tag>;
    case 10:
      return <Tag color='gold'>{t('user.table.role_types.admin')}</Tag>;
    case 100:
      return <Tag color='orange'>{t('user.table.role_types.super_admin')}</Tag>;
    default:
      return <Tag color='red'>{t('user.table.role_types.unknown')}</Tag>;
  }
}

const UsersTable = () => {
  const { t } = useTranslation();
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [searching, setSearching] = useState(false);
  const [orderBy, setOrderBy] = useState('');
  // Manual top-up: the operator grants quota through /api/topup, which also
  // writes a top-up log entry — distinct from editing the quota field on the
  // user record, which bypasses the ledger note.
  const [topupUser, setTopupUser] = useState(null);
  const [topupQuota, setTopupQuota] = useState(0);
  const [topupRemark, setTopupRemark] = useState('');
  const [topupSubmitting, setTopupSubmitting] = useState(false);

  const loadUsers = async (startIdx) => {
    const res = await API.get(`/api/user/?p=${startIdx}&order=${orderBy}`);
    const { success, message, data } = res.data;
    if (success) {
      if (startIdx === 0) {
        setUsers(data);
      } else {
        let newUsers = [...users];
        newUsers.push(...data);
        setUsers(newUsers);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (page: number) => {
    (async () => {
      if (page === Math.ceil(users.length / ITEMS_PER_PAGE) + 1) {
        // In this case we have to load more data and then append them.
        await loadUsers(page - 1);
      }
      setActivePage(page);
    })();
  };

  useEffect(() => {
    loadUsers(0)
      .then()
      .catch((reason) => {
        showError(reason);
      });
  }, [orderBy]);

  const manageUser = (username, action, idx) => {
    (async () => {
      const res = await API.post('/api/user/manage', {
        username,
        action,
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('user.messages.operation_success'));
        let user = res.data.data;
        let newUsers = [...users];
        let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
        if (action === 'delete') {
          newUsers[realIdx].deleted = true;
        } else {
          newUsers[realIdx].status = user.status;
          newUsers[realIdx].role = user.role;
        }
        setUsers(newUsers);
      } else {
        showError(message);
      }
    })();
  };

  const submitTopup = async () => {
    if (!topupUser || topupQuota <= 0) {
      showError(t('user.topup.invalid_quota'));
      return;
    }
    setTopupSubmitting(true);
    try {
      const res = await API.post('/api/topup', {
        user_id: topupUser.id,
        quota: topupQuota,
        remark: topupRemark,
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('user.topup.success'));
        const refreshed = await API.get(
          `/api/user/${topupUser.id}`
        );
        const fresh = refreshed.data;
        if (fresh.success) {
          const newUsers = [...users];
          const realIdx = newUsers.findIndex((u) => u.id === topupUser.id);
          if (realIdx >= 0) {
            newUsers[realIdx].quota = fresh.data.quota;
            setUsers(newUsers);
          }
        }
        setTopupUser(null);
        setTopupQuota(0);
        setTopupRemark('');
      } else {
        showError(message);
      }
    } catch (reason) {
      showError(reason);
    } finally {
      setTopupSubmitting(false);
    }
  };

  const renderStatus = (status) => {
    switch (status) {
      case 1:
        return <Tag>{t('user.table.status_types.activated')}</Tag>;
      case 2:
        return <Tag color='red'>{t('user.table.status_types.banned')}</Tag>;
      default:
        return <Tag>{t('user.table.status_types.unknown')}</Tag>;
    }
  };

  const searchUsers = async () => {
    if (searchKeyword === '') {
      // if keyword is blank, load files instead.
      await loadUsers(0);
      setActivePage(1);
      setOrderBy('');
      return;
    }
    setSearching(true);
    const res = await API.get(`/api/user/search?keyword=${searchKeyword}`);
    const { success, message, data } = res.data;
    if (success) {
      setUsers(data);
      setActivePage(1);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  const sortUser = (key) => {
    if (users.length === 0) return;
    setLoading(true);
    let sortedUsers = [...users];
    sortedUsers.sort((a, b) => {
      if (!isNaN(a[key])) {
        // If the value is numeric, subtract to sort
        return a[key] - b[key];
      } else {
        // If the value is not numeric, sort as strings
        return ('' + a[key]).localeCompare(b[key]);
      }
    });
    if (sortedUsers[0].id === users[0].id) {
      sortedUsers.reverse();
    }
    setUsers(sortedUsers);
    setLoading(false);
  };

  const handleOrderByChange = (value) => {
    setOrderBy(value);
    setActivePage(1);
  };

  const sortableTitle = (key, label) => (
    <span style={{ cursor: 'pointer' }} onClick={() => sortUser(key)}>
      {label}
    </span>
  );

  const pageUsers = users
    .slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE)
    .map((user, idx) => ({ ...user, __idx: idx }))
    .filter((user) => !user.deleted);

  const columns = [
    {
      title: sortableTitle('id', t('user.table.id')),
      dataIndex: 'id',
      width: 60,
    },
    {
      title: sortableTitle('username', t('user.table.username')),
      dataIndex: 'username',
      render: (username, user) => (
        <Tooltip
          title={user.email ? user.email : '未绑定邮箱地址'}
        >
          <span>{renderText(username, 15)}</span>
        </Tooltip>
      ),
    },
    {
      title: sortableTitle('group', t('user.table.group')),
      dataIndex: 'group',
      render: (group) => renderGroup(group),
    },
    {
      title: sortableTitle('quota', t('user.table.quota')),
      dataIndex: 'quota',
      render: (_, user) => (
        <Space size={4} wrap>
          <Tooltip title={t('user.table.remaining_quota')}>
            <Tag>{renderQuota(user.quota, t)}</Tag>
          </Tooltip>
          <Tooltip title={t('user.table.used_quota')}>
            <Tag>{renderQuota(user.used_quota, t)}</Tag>
          </Tooltip>
          <Tooltip title={t('user.table.request_count')}>
            <Tag>{renderNumber(user.request_count)}</Tag>
          </Tooltip>
        </Space>
      ),
    },
    {
      title: sortableTitle('role', t('user.table.role_text')),
      dataIndex: 'role',
      render: (role) => renderRole(role, t),
    },
    {
      title: sortableTitle('status', t('user.table.status_text')),
      dataIndex: 'status',
      render: (status) => renderStatus(status),
    },
    {
      title: t('user.table.actions'),
      key: 'actions',
      render: (_, user) => {
        const idx = user.__idx;
        return (
          <Space size={4} wrap>
            <Button
              size='small'
              disabled={user.role === 100}
              onClick={() => manageUser(user.username, 'promote', idx)}
            >
              {t('user.buttons.promote')}
            </Button>
            <Button
              size='small'
              disabled={user.role === 100}
              onClick={() => manageUser(user.username, 'demote', idx)}
            >
              {t('user.buttons.demote')}
            </Button>
            <Popconfirm
              title={`${t('user.buttons.delete_user')} ${user.username}`}
              onConfirm={() => manageUser(user.username, 'delete', idx)}
              disabled={user.role === 100}
            >
              <Button size='small' danger disabled={user.role === 100}>
                {t('user.buttons.delete')}
              </Button>
            </Popconfirm>
            <Button
              size='small'
              disabled={user.role === 100}
              onClick={() =>
                manageUser(
                  user.username,
                  user.status === 1 ? 'disable' : 'enable',
                  idx
                )
              }
            >
              {user.status === 1
                ? t('user.buttons.disable')
                : t('user.buttons.enable')}
            </Button>
            <Link to={'/user/edit/' + user.id}>
              <Button size='small'>{t('user.buttons.edit')}</Button>
            </Link>
            <Button
              size='small'
              type='primary'
              ghost
              onClick={() => {
                setTopupUser(user);
                setTopupQuota(0);
                setTopupRemark('');
              }}
            >
              {t('user.buttons.topup')}
            </Button>
          </Space>
        );
      },
    },
  ];

  return (
    <>
      <Input
        allowClear
        prefix={<SearchOutlined />}
        placeholder={t('user.search')}
        value={searchKeyword}
        onChange={(e) => setSearchKeyword(e.target.value.trim())}
        onPressEnter={searchUsers}
        suffix={
          <Button type='link' size='small' loading={searching} onClick={searchUsers}>
            {t('user.search')}
          </Button>
        }
        style={{ maxWidth: 360, marginBottom: 16 }}
      />

      <Space style={{ marginBottom: 16 }} wrap>
        <Link to='/user/add'>
          <Button type='primary' loading={loading}>
            {t('user.buttons.add')}
          </Button>
        </Link>
        <Select
          placeholder={t('user.table.sort_by')}
          value={orderBy || undefined}
          style={{ minWidth: 160 }}
          allowClear
          options={[
            { label: t('user.table.sort.default'), value: '' },
            { label: t('user.table.sort.by_quota'), value: 'quota' },
            { label: t('user.table.sort.by_used_quota'), value: 'used_quota' },
            {
              label: t('user.table.sort.by_request_count'),
              value: 'request_count',
            },
          ]}
          onChange={handleOrderByChange}
        />
      </Space>

      <Table
        size='small'
        columns={columns}
        dataSource={pageUsers}
        rowKey='id'
        pagination={{
          current: activePage,
          pageSize: ITEMS_PER_PAGE,
          total: users.length,
          showSizeChanger: false,
          onChange: onPaginationChange,
        }}
      />

      <Modal
        open={topupUser !== null}
        onCancel={() => setTopupUser(null)}
        title={t('user.topup.title', { name: topupUser ? topupUser.username : '' })}
        footer={[
          <Button key='cancel' onClick={() => setTopupUser(null)}>
            {t('user.topup.cancel')}
          </Button>,
          <Button
            key='submit'
            type='primary'
            loading={topupSubmitting}
            onClick={() => void submitTopup()}
          >
            {t('user.topup.confirm')}
          </Button>,
        ]}
      >
        <Space direction='vertical' style={{ width: '100%' }} size={16}>
          <QuotaInput
            label={t('user.topup.quota')}
            name='quota'
            quota={topupQuota}
            onQuotaChange={(name, value) => setTopupQuota(value)}
            placeholder={t('user.topup.quota_placeholder')}
          />
          <div>
            <Typography.Text type='secondary' style={{ display: 'block', marginBottom: 4 }}>
              {t('user.topup.remark')}
            </Typography.Text>
            <Input
              placeholder={t('user.topup.remark_placeholder')}
              onChange={(e) => setTopupRemark(e.target.value)}
              value={topupRemark}
              autoComplete='new-password'
            />
          </div>
        </Space>
      </Modal>
    </>
  );
};

export default UsersTable;
