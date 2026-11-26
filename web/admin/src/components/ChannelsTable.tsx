import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import {
  API,
  loadChannelModels,
  setPromptShown,
  shouldShowPrompt,
  showError,
  showInfo,
  showSuccess,
  timestamp2string,
} from '../helpers';

import { CHANNEL_OPTIONS, ITEMS_PER_PAGE } from '../constants';
import { renderGroup } from '../helpers/render';

let type2label = undefined;

function getTypeLabel(type, t) {
  if (!type2label) {
    type2label = {};
    for (let i = 0; i < CHANNEL_OPTIONS.length; i++) {
      type2label[CHANNEL_OPTIONS[i].value] = CHANNEL_OPTIONS[i];
    }
    type2label[0] = {
      value: 0,
      text: t('channel.table.status_unknown'),
      color: 'default',
    };
  }
  return type2label[type];
}

function renderType(type, t) {
  const label = getTypeLabel(type, t);
  return <Tag color={label?.color}>{label ? label.text : type}</Tag>;
}

function renderBalance(type, balance, t) {
  switch (type) {
    case 1: // OpenAI
      if (balance === 0) {
        return <span>{t('channel.table.balance_not_supported')}</span>;
      }
      return <span>${balance.toFixed(2)}</span>;
    default:
      return <span>{t('channel.table.balance_not_supported')}</span>;
  }
}

function isShowDetail() {
  return localStorage.getItem('show_detail') === 'true';
}

const promptID = 'detail';

const ChannelsTable = () => {
  const { t } = useTranslation();
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [searching, setSearching] = useState(false);
  const [updatingBalance, setUpdatingBalance] = useState(false);
  const [showPrompt, setShowPrompt] = useState(shouldShowPrompt(promptID));
  const [showDetail, setShowDetail] = useState(isShowDetail());

  const processChannelData = (channel) => {
    if (channel.models === '') {
      channel.models = [];
      channel.test_model = '';
    } else {
      channel.models = channel.models.split(',');
      if (channel.models.length > 0) {
        channel.test_model = channel.models[0];
      }
      channel.model_options = channel.models.map((model) => {
        return {
          key: model,
          label: model,
          value: model,
        };
      });
    }
    return channel;
  };

  const loadChannels = async (startIdx) => {
    const res = await API.get(`/api/channel/?p=${startIdx}`);
    const { success, message, data } = res.data;
    if (success) {
      let localChannels = data.map(processChannelData);
      if (startIdx === 0) {
        setChannels(localChannels);
      } else {
        let newChannels = [...channels];
        newChannels.splice(
          startIdx * ITEMS_PER_PAGE,
          data.length,
          ...localChannels
        );
        setChannels(newChannels);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (page: number) => {
    (async () => {
      if (page === Math.ceil(channels.length / ITEMS_PER_PAGE) + 1) {
        // In this case we have to load more data and then append them.
        await loadChannels(page - 1);
      }
      setActivePage(page);
    })();
  };

  const refresh = async () => {
    setLoading(true);
    await loadChannels(activePage - 1);
  };

  const toggleShowDetail = () => {
    setShowDetail(!showDetail);
    localStorage.setItem('show_detail', (!showDetail).toString());
  };

  useEffect(() => {
    loadChannels(0)
      .then()
      .catch((reason) => {
        showError(reason);
      });
    loadChannelModels().then();
  }, []);

  const manageChannel = async (id, action, idx, value = '') => {
    const data: { id: number; status?: number; priority?: number; weight?: number } = { id };
    let res;
    switch (action) {
      case 'delete':
        res = await API.delete(`/api/channel/${id}/`);
        break;
      case 'enable':
        data.status = 1;
        res = await API.put('/api/channel/', data);
        break;
      case 'disable':
        data.status = 2;
        res = await API.put('/api/channel/', data);
        break;
      case 'priority':
        if (value === '') {
          return;
        }
        data.priority = parseInt(value);
        res = await API.put('/api/channel/', data);
        break;
      case 'weight':
        if (value === '') {
          return;
        }
        data.weight = parseInt(value);
        if (data.weight < 0) {
          data.weight = 0;
        }
        res = await API.put('/api/channel/', data);
        break;
    }
    const { success, message } = res.data;
    if (success) {
      showSuccess(t('channel.messages.operation_success'));
      let channel = res.data.data;
      let newChannels = [...channels];
      let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
      if (action === 'delete') {
        newChannels[realIdx].deleted = true;
      } else {
        newChannels[realIdx].status = channel.status;
      }
      setChannels(newChannels);
    } else {
      showError(message);
    }
  };

  const renderStatus = (status) => {
    switch (status) {
      case 1:
        return <Tag color='green'>{t('channel.table.status_enabled')}</Tag>;
      case 2:
        return (
          <Tooltip title={t('channel.table.status_disabled_tip')}>
            <Tag color='red'>{t('channel.table.status_disabled')}</Tag>
          </Tooltip>
        );
      case 3:
        return (
          <Tooltip title={t('channel.table.status_auto_disabled_tip')}>
            <Tag color='gold'>{t('channel.table.status_auto_disabled')}</Tag>
          </Tooltip>
        );
      default:
        return <Tag>{t('channel.table.status_unknown')}</Tag>;
    }
  };

  const renderResponseTime = (channel) => {
    const responseTime = channel.response_time;
    const time = (responseTime / 1000).toFixed(2) + 's';
    let tag;
    if (responseTime === 0) {
      tag = <Tag>{t('channel.table.not_tested')}</Tag>;
    } else if (responseTime <= 1000) {
      tag = <Tag color='green'>{time}</Tag>;
    } else if (responseTime <= 3000) {
      tag = <Tag color='lime'>{time}</Tag>;
    } else if (responseTime <= 5000) {
      tag = <Tag color='gold'>{time}</Tag>;
    } else {
      tag = <Tag color='red'>{time}</Tag>;
    }
    return (
      <Tooltip
        title={
          channel.test_time
            ? timestamp2string(channel.test_time)
            : t('channel.table.not_tested')
        }
      >
        {tag}
      </Tooltip>
    );
  };

  const searchChannels = async () => {
    if (searchKeyword === '') {
      // if keyword is blank, load files instead.
      await loadChannels(0);
      setActivePage(1);
      return;
    }
    setSearching(true);
    const res = await API.get(`/api/channel/search?keyword=${searchKeyword}`);
    const { success, message, data } = res.data;
    if (success) {
      let localChannels = data.map(processChannelData);
      setChannels(localChannels);
      setActivePage(1);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  const switchTestModel = async (idx, model) => {
    let newChannels = [...channels];
    let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
    newChannels[realIdx].test_model = model;
    setChannels(newChannels);
  };

  const testChannel = async (id, name, idx, m) => {
    let res;
    try {
      res = await API.get(`/api/channel/test/${id}?model=${m}`);
    } catch (error) {
      // interceptor toasts the error
      return;
    }
    const { success, message, time, model } = res.data;
    if (success) {
      let newChannels = [...channels];
      let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
      newChannels[realIdx].response_time = time * 1000;
      newChannels[realIdx].test_time = Date.now() / 1000;
      setChannels(newChannels);
      showSuccess(
        t('channel.messages.test_success', { name, model, time, message })
      );
    } else {
      showError(message);
    }
  };

  const testChannels = async (scope) => {
    const res = await API.get(`/api/channel/test?scope=${scope}`);
    const { success, message } = res.data;
    if (success) {
      showInfo(t('channel.messages.test_all_started'));
    } else {
      showError(message);
    }
  };

  const deleteAllDisabledChannels = async () => {
    const res = await API.delete(`/api/channel/disabled`);
    const { success, message, data } = res.data;
    if (success) {
      showSuccess(
        t('channel.messages.delete_disabled_success', { count: data })
      );
      await refresh();
    } else {
      showError(message);
    }
  };

  const updateChannelBalance = async (id, name, idx) => {
    const res = await API.get(`/api/channel/update_balance/${id}/`);
    const { success, message, balance } = res.data;
    if (success) {
      let newChannels = [...channels];
      let realIdx = (activePage - 1) * ITEMS_PER_PAGE + idx;
      newChannels[realIdx].balance = balance;
      newChannels[realIdx].balance_updated_time = Date.now() / 1000;
      setChannels(newChannels);
      showSuccess(t('channel.messages.balance_update_success', { name }));
    } else {
      showError(message);
    }
  };

  const updateAllChannelsBalance = async () => {
    setUpdatingBalance(true);
    const res = await API.get(`/api/channel/update_balance`);
    const { success, message } = res.data;
    if (success) {
      showInfo(t('channel.messages.all_balance_updated'));
    } else {
      showError(message);
    }
    setUpdatingBalance(false);
  };

  const sortChannel = (key) => {
    if (channels.length === 0) return;
    setLoading(true);
    let sortedChannels = [...channels];
    sortedChannels.sort((a, b) => {
      if (!isNaN(a[key])) {
        // If the value is numeric, subtract to sort
        return a[key] - b[key];
      } else {
        // If the value is not numeric, sort as strings
        return ('' + a[key]).localeCompare(b[key]);
      }
    });
    if (sortedChannels[0].id === channels[0].id) {
      sortedChannels.reverse();
    }
    setChannels(sortedChannels);
    setLoading(false);
  };

  const sortableTitle = (key, label) => (
    <span style={{ cursor: 'pointer' }} onClick={() => sortChannel(key)}>
      {label}
    </span>
  );

  const columns = [
    {
      title: sortableTitle('id', t('channel.table.id')),
      dataIndex: 'id',
      width: 60,
    },
    {
      title: sortableTitle('name', t('channel.table.name')),
      dataIndex: 'name',
      render: (name) => (name ? name : t('channel.table.no_name')),
    },
    {
      title: sortableTitle('group', t('channel.table.group')),
      dataIndex: 'group',
      render: (group) => renderGroup(group),
    },
    {
      title: sortableTitle('type', t('channel.table.type')),
      dataIndex: 'type',
      render: (type) => renderType(type, t),
    },
    {
      title: sortableTitle('status', t('channel.table.status')),
      dataIndex: 'status',
      render: (status) => renderStatus(status),
    },
    {
      title: sortableTitle('response_time', t('channel.table.response_time')),
      dataIndex: 'response_time',
      render: (_, channel) => renderResponseTime(channel),
    },
    {
      title: sortableTitle('balance', t('channel.table.balance')),
      dataIndex: 'balance',
      render: (balance, channel) => (
        <Tooltip title={t('channel.table.click_to_update')}>
          <span
            onClick={() => updateChannelBalance(channel.id, channel.name, channel.__idx)}
            style={{ cursor: 'pointer' }}
          >
            {renderBalance(channel.type, balance, t)}
          </span>
        </Tooltip>
      ),
    },
    {
      title: sortableTitle('priority', t('channel.table.priority')),
      dataIndex: 'priority',
      hidden: !showDetail,
      render: (value, channel) => (
        <Tooltip title={t('channel.table.priority_tip')}>
          <InputNumber
            size='small'
            defaultValue={channel.priority}
            style={{ width: 70 }}
            onBlur={(event) => {
              manageChannel(
                channel.id,
                'priority',
                channel.__idx,
                event.target.value
              );
            }}
          />
        </Tooltip>
      ),
    },
    {
      title: t('channel.table.test_model'),
      dataIndex: 'test_model',
      hidden: !showDetail,
      render: (_, channel) => (
        <Select
          size='small'
          style={{ minWidth: 140 }}
          placeholder={t('channel.table.select_test_model')}
          options={channel.model_options}
          value={channel.test_model || undefined}
          onChange={(value) => switchTestModel(channel.__idx, value)}
        />
      ),
    },
    {
      title: t('channel.table.actions'),
      key: 'actions',
      render: (_, channel) => {
        const idx = channel.__idx;
        return (
          <Space size={4} wrap>
            <Button
              size='small'
              color='green'
              variant='solid'
              onClick={() => testChannel(channel.id, channel.name, idx, channel.test_model)}
            >
              {t('channel.buttons.test')}
            </Button>
            <Button
              size='small'
              onClick={() =>
                manageChannel(
                  channel.id,
                  channel.status === 1 ? 'disable' : 'enable',
                  idx
                )
              }
            >
              {channel.status === 1
                ? t('channel.buttons.disable')
                : t('channel.buttons.enable')}
            </Button>
            <Link to={'/channel/edit/' + channel.id}>
              <Button size='small'>{t('channel.buttons.edit')}</Button>
            </Link>
            <Button
              size='small'
              danger
              onClick={() => manageChannel(channel.id, 'delete', idx)}
            >
              {t('channel.buttons.delete')}
            </Button>
          </Space>
        );
      },
    },
  ].filter((column) => !column.hidden);

  const pageChannels = channels
    .slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE)
    .map((channel, idx) => ({ ...channel, __idx: idx }))
    .filter((channel) => !channel.deleted);

  return (
    <>
      <Input
        allowClear
        prefix={<SearchOutlined />}
        placeholder={t('channel.search')}
        value={searchKeyword}
        onChange={(e) => setSearchKeyword(e.target.value.trim())}
        onPressEnter={searchChannels}
        suffix={
          <Button type='link' size='small' loading={searching} onClick={searchChannels}>
            {t('channel.search')}
          </Button>
        }
        style={{ maxWidth: 360, marginBottom: 16 }}
      />
      {showPrompt && (
        <Alert
          type='info'
          closable
          afterClose={() => setPromptShown(promptID)}
          message={
            <>
              {t('channel.balance_notice')}
              <br />
              {t('channel.test_notice')}
              <br />
              {t('channel.detail_notice')}
            </>
          }
          style={{ marginBottom: 16 }}
        />
      )}
      <Space style={{ marginBottom: 16 }} wrap>
        <Link to='/channel/add'>
          <Button type='primary' loading={loading}>
            {t('channel.buttons.add')}
          </Button>
        </Link>
        <Button loading={loading} onClick={() => testChannels('all')}>
          {t('channel.buttons.test_all')}
        </Button>
        <Button loading={loading} onClick={() => testChannels('disabled')}>
          {t('channel.buttons.test_disabled')}
        </Button>
        <Popconfirm
          title={t('channel.buttons.confirm_delete_disabled')}
          onConfirm={deleteAllDisabledChannels}
        >
          <Button danger loading={loading}>
            {t('channel.buttons.delete_disabled')}
          </Button>
        </Popconfirm>
        <Button loading={loading} onClick={refresh}>
          {t('channel.buttons.refresh')}
        </Button>
        <Button onClick={toggleShowDetail}>
          {showDetail
            ? t('channel.buttons.hide_detail')
            : t('channel.buttons.show_detail')}
        </Button>
      </Space>
      <Table
        size='small'
        columns={columns}
        dataSource={pageChannels}
        rowKey='id'
        pagination={{
          current: activePage,
          pageSize: ITEMS_PER_PAGE,
          total: channels.length,
          showSizeChanger: false,
          onChange: onPaginationChange,
        }}
      />
    </>
  );
};

export default ChannelsTable;
