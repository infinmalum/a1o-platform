import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Input,
  Popconfirm,
  Space,
  Table,
  Tag,
} from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import {
  API,
  copy,
  showError,
  showSuccess,
  showWarning,
  timestamp2string,
} from '../helpers';

import { ITEMS_PER_PAGE } from '../constants';
import { renderQuota } from '../helpers/render';

function renderStatus(status, t) {
  switch (status) {
    case 1:
      return <Tag color='green'>{t('redemption.status.unused')}</Tag>;
    case 2:
      return <Tag color='red'>{t('redemption.status.disabled')}</Tag>;
    case 3:
      return <Tag>{t('redemption.status.used')}</Tag>;
    default:
      return <Tag>{t('redemption.status.unknown')}</Tag>;
  }
}

const RedemptionsTable = () => {
  const { t } = useTranslation();
  const [redemptions, setRedemptions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [searching, setSearching] = useState(false);

  const loadRedemptions = async (startIdx) => {
    const res = await API.get(`/api/redemption/?p=${startIdx}`);
    const { success, message, data } = res.data;
    if (success) {
      if (startIdx === 0) {
        setRedemptions(data);
      } else {
        let newRedemptions = [...redemptions];
        newRedemptions.push(...data);
        setRedemptions(newRedemptions);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (page: number) => {
    (async () => {
      if (page === Math.ceil(redemptions.length / ITEMS_PER_PAGE) + 1) {
        // In this case we have to load more data and then append them.
        await loadRedemptions(page - 1);
      }
      setActivePage(page);
    })();
  };

  useEffect(() => {
    loadRedemptions(0)
      .then()
      .catch((reason) => {
        showError(reason);
      });
  }, []);

  const manageRedemption = async (id, action, idx) => {
    const data: { id: number; status?: number } = { id };
    let res;
    switch (action) {
      case 'delete':
        res = await API.delete(`/api/redemption/${id}/`);
        break;
      case 'enable':
        data.status = 1;
        res = await API.put('/api/redemption/?status_only=true', data);
        break;
      case 'disable':
        data.status = 2;
        res = await API.put('/api/redemption/?status_only=true', data);
        break;
    }
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('token.messages.operation_success'));
      let redemption = res.data.data;
      let newRedemptions = [...redemptions];
      let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
      if (action === 'delete') {
        newRedemptions[realIdx].deleted = true;
      } else {
        newRedemptions[realIdx].status = redemption.status;
      }
      setRedemptions(newRedemptions);
    } else {
      showError(message);
    }
  };

  const searchRedemptions = async () => {
    if (searchKeyword === '') {
      // if keyword is blank, load files instead.
      await loadRedemptions(0);
      setActivePage(1);
      return;
    }
    setSearching(true);
    const res = await API.get(
      `/api/redemption/search?keyword=${searchKeyword}`
    );
    const { success, message, data } = res.data;
    if (success) {
      setRedemptions(data);
      setActivePage(1);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  const sortRedemption = (key) => {
    if (redemptions.length === 0) return;
    setLoading(true);
    let sortedRedemptions = [...redemptions];
    sortedRedemptions.sort((a, b) => {
      if (!isNaN(a[key])) {
        // If the value is numeric, subtract to sort
        return a[key] - b[key];
      } else {
        // If the value is not numeric, sort as strings
        return ('' + a[key]).localeCompare(b[key]);
      }
    });
    if (sortedRedemptions[0].id === redemptions[0].id) {
      sortedRedemptions.reverse();
    }
    setRedemptions(sortedRedemptions);
    setLoading(false);
  };

  const refresh = async () => {
    setLoading(true);
    await loadRedemptions(0);
    setActivePage(1);
  };

  const sortableTitle = (key, label) => (
    <span style={{ cursor: 'pointer' }} onClick={() => sortRedemption(key)}>
      {label}
    </span>
  );

  const pageRedemptions = redemptions
    .slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE)
    .map((redemption, idx) => ({ ...redemption, __idx: idx }))
    .filter((redemption) => !redemption.deleted);

  const columns = [
    {
      title: sortableTitle('id', t('redemption.table.id')),
      dataIndex: 'id',
      width: 60,
    },
    {
      title: sortableTitle('name', t('redemption.table.name')),
      dataIndex: 'name',
      render: (name) => (name ? name : t('redemption.table.no_name')),
    },
    {
      title: sortableTitle('status', t('redemption.table.status')),
      dataIndex: 'status',
      render: (status) => renderStatus(status, t),
    },
    {
      title: sortableTitle('quota', t('redemption.table.quota')),
      dataIndex: 'quota',
      render: (quota) => renderQuota(quota, t),
    },
    {
      title: sortableTitle('created_time', t('redemption.table.created_time')),
      dataIndex: 'created_time',
      render: (value) => timestamp2string(value),
    },
    {
      title: sortableTitle('redeemed_time', t('redemption.table.redeemed_time')),
      dataIndex: 'redeemed_time',
      render: (value) =>
        value ? timestamp2string(value) : t('redemption.table.not_redeemed'),
    },
    {
      title: t('redemption.table.actions'),
      key: 'actions',
      render: (_, redemption) => {
        const idx = redemption.__idx;
        return (
          <Space size={4} wrap>
            <Button
              size='small'
              type='primary'
              ghost
              onClick={async () => {
                if (await copy(redemption.key)) {
                  showSuccess(t('token.messages.copy_success'));
                } else {
                  showWarning(t('token.messages.copy_failed'));
                  setSearchKeyword(redemption.key);
                }
              }}
            >
              {t('redemption.buttons.copy')}
            </Button>
            <Popconfirm
              title={t('redemption.buttons.confirm_delete')}
              onConfirm={() => manageRedemption(redemption.id, 'delete', idx)}
            >
              <Button size='small' danger>
                {t('redemption.buttons.delete')}
              </Button>
            </Popconfirm>
            <Button
              size='small'
              disabled={redemption.status === 3} // used
              onClick={() => {
                manageRedemption(
                  redemption.id,
                  redemption.status === 1 ? 'disable' : 'enable',
                  idx
                );
              }}
            >
              {redemption.status === 1
                ? t('redemption.buttons.disable')
                : t('redemption.buttons.enable')}
            </Button>
            <Link to={'/redemption/edit/' + redemption.id}>
              <Button size='small'>{t('redemption.buttons.edit')}</Button>
            </Link>
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
        placeholder={t('redemption.search')}
        value={searchKeyword}
        onChange={(e) => setSearchKeyword(e.target.value.trim())}
        onPressEnter={searchRedemptions}
        suffix={
          <Button type='link' size='small' loading={searching} onClick={searchRedemptions}>
            {t('redemption.search')}
          </Button>
        }
        style={{ maxWidth: 360, marginBottom: 16 }}
      />

      <Space style={{ marginBottom: 16 }}>
        <Link to='/redemption/add'>
          <Button type='primary' loading={loading}>
            {t('redemption.buttons.add')}
          </Button>
        </Link>
        <Button onClick={refresh} loading={loading}>
          {t('redemption.buttons.refresh')}
        </Button>
      </Space>

      <Table
        size='small'
        columns={columns}
        dataSource={pageRedemptions}
        rowKey='id'
        pagination={{
          current: activePage,
          pageSize: ITEMS_PER_PAGE,
          total: redemptions.length,
          showSizeChanger: false,
          onChange: onPaginationChange,
        }}
      />
    </>
  );
};

export default RedemptionsTable;
