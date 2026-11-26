import React, { useEffect, useState } from 'react';
import {
  Button,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import {
  API,
  copy,
  isAdmin,
  showError,
  showSuccess,
  showWarning,
  timestamp2string,
} from '../helpers';
import { useTranslation } from 'react-i18next';

import { ITEMS_PER_PAGE } from '../constants';
import { renderColorLabel, renderQuota } from '../helpers/render';
import { Link } from 'react-router-dom';

function renderTimestamp(timestamp, request_id) {
  return (
    <code
      onClick={async () => {
        if (await copy(request_id)) {
          showSuccess(`已复制请求 ID：${request_id}`);
        } else {
          showWarning(`请求 ID 复制失败：${request_id}`);
        }
      }}
      style={{ cursor: 'pointer' }}
    >
      {timestamp2string(timestamp)}
    </code>
  );
}

function renderType(type) {
  switch (type) {
    case 1:
      return <Tag color='green'>充值</Tag>;
    case 2:
      return <Tag color='lime'>消费</Tag>;
    case 3:
      return <Tag color='orange'>管理</Tag>;
    case 4:
      return <Tag color='purple'>系统</Tag>;
    case 5:
      return <Tag color='geekblue'>测试</Tag>;
    default:
      return <Tag>未知</Tag>;
  }
}

function getColorByElapsedTime(elapsedTime) {
  if (elapsedTime === undefined || 0) return 'default';
  if (elapsedTime < 1000) return 'green';
  if (elapsedTime < 3000) return 'lime';
  if (elapsedTime < 5000) return 'gold';
  if (elapsedTime < 10000) return 'orange';
  return 'red';
}

function renderDetail(log) {
  return (
    <>
      {log.content}
      <br />
      {log.elapsed_time && (
        <Tag color={getColorByElapsedTime(log.elapsed_time)}>
          {log.elapsed_time} ms
        </Tag>
      )}
      {log.is_stream && <Tag color='magenta'>Stream</Tag>}
      {log.system_prompt_reset && (
        <Tag color='red'>System Prompt Reset</Tag>
      )}
    </>
  );
}

const LogsTable = () => {
  const { t } = useTranslation();
  const [logs, setLogs] = useState([]);
  const [showStat, setShowStat] = useState(false);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [searching, setSearching] = useState(false);
  const [logType, setLogType] = useState(0);
  const isAdminUser = isAdmin();
  let now = new Date();
  const [inputs, setInputs] = useState({
    username: '',
    token_name: '',
    model_name: '',
    start_timestamp: timestamp2string(0),
    end_timestamp: timestamp2string(now.getTime() / 1000 + 3600),
    channel: '',
  });
  const {
    username,
    token_name,
    model_name,
    start_timestamp,
    end_timestamp,
    channel,
  } = inputs;

  const [stat, setStat] = useState({
    quota: 0,
    token: 0,
  });

  const LOG_OPTIONS = [
    { key: '0', label: t('log.type.all'), value: 0 },
    { key: '1', label: t('log.type.topup'), value: 1 },
    { key: '2', label: t('log.type.usage'), value: 2 },
    { key: '3', label: t('log.type.admin'), value: 3 },
    { key: '4', label: t('log.type.system'), value: 4 },
    { key: '5', label: t('log.type.test'), value: 5 },
  ];

  const handleInputChange = (name) => (e) => {
    setInputs((inputs) => ({ ...inputs, [name]: e.target.value }));
  };

  const getLogSelfStat = async () => {
    let localStartTimestamp = Date.parse(start_timestamp) / 1000;
    let localEndTimestamp = Date.parse(end_timestamp) / 1000;
    let res = await API.get(
      `/api/log/self/stat?type=${logType}&token_name=${token_name}&model_name=${model_name}&start_timestamp=${localStartTimestamp}&end_timestamp=${localEndTimestamp}`
    );
    const { success, message, data } = res.data;
    if (success) {
      setStat(data);
    } else {
      showError(message);
    }
  };

  const getLogStat = async () => {
    let localStartTimestamp = Date.parse(start_timestamp) / 1000;
    let localEndTimestamp = Date.parse(end_timestamp) / 1000;
    let res = await API.get(
      `/api/log/stat?type=${logType}&username=${username}&token_name=${token_name}&model_name=${model_name}&start_timestamp=${localStartTimestamp}&end_timestamp=${localEndTimestamp}&channel=${channel}`
    );
    const { success, message, data } = res.data;
    if (success) {
      setStat(data);
    } else {
      showError(message);
    }
  };

  const handleEyeClick = async () => {
    if (!showStat) {
      if (isAdminUser) {
        await getLogStat();
      } else {
        await getLogSelfStat();
      }
    }
    setShowStat(!showStat);
  };

  const showUserTokenQuota = () => {
    return logType !== 5;
  };

  const loadLogs = async (startIdx) => {
    let url = '';
    let localStartTimestamp = Date.parse(start_timestamp) / 1000;
    let localEndTimestamp = Date.parse(end_timestamp) / 1000;
    if (isAdminUser) {
      url = `/api/log/?p=${startIdx}&type=${logType}&username=${username}&token_name=${token_name}&model_name=${model_name}&start_timestamp=${localStartTimestamp}&end_timestamp=${localEndTimestamp}&channel=${channel}`;
    } else {
      url = `/api/log/self/?p=${startIdx}&type=${logType}&token_name=${token_name}&model_name=${model_name}&start_timestamp=${localStartTimestamp}&end_timestamp=${localEndTimestamp}`;
    }
    const res = await API.get(url);
    const { success, message, data } = res.data;
    if (success) {
      if (startIdx === 0) {
        setLogs(data);
      } else {
        let newLogs = [...logs];
        newLogs.splice(startIdx * ITEMS_PER_PAGE, data.length, ...data);
        setLogs(newLogs);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const onPaginationChange = (page: number) => {
    (async () => {
      if (page === Math.ceil(logs.length / ITEMS_PER_PAGE) + 1) {
        // In this case we have to load more data and then append them.
        await loadLogs(page - 1);
      }
      setActivePage(page);
    })();
  };

  const refresh = async () => {
    setLoading(true);
    setActivePage(1);
    await loadLogs(0);
  };

  useEffect(() => {
    refresh().then();
  }, [logType]);

  const searchLogs = async () => {
    if (searchKeyword === '') {
      // if keyword is blank, load files instead.
      await loadLogs(0);
      setActivePage(1);
      return;
    }
    setSearching(true);
    const res = await API.get(`/api/log/self/search?keyword=${searchKeyword}`);
    const { success, message, data } = res.data;
    if (success) {
      setLogs(data);
      setActivePage(1);
    } else {
      showError(message);
    }
    setSearching(false);
  };

  const sortLog = (key) => {
    if (logs.length === 0) return;
    setLoading(true);
    let sortedLogs = [...logs];
    if (typeof sortedLogs[0][key] === 'string') {
      sortedLogs.sort((a, b) => {
        return ('' + a[key]).localeCompare(b[key]);
      });
    } else {
      sortedLogs.sort((a, b) => {
        if (a[key] === b[key]) return 0;
        if (a[key] > b[key]) return -1;
        if (a[key] < b[key]) return 1;
      });
    }
    if (sortedLogs[0].id === logs[0].id) {
      sortedLogs.reverse();
    }
    setLogs(sortedLogs);
    setLoading(false);
  };

  const sortableTitle = (key, label) => (
    <span style={{ cursor: 'pointer' }} onClick={() => sortLog(key)}>
      {label}
    </span>
  );

  const columns = [
    {
      title: sortableTitle('created_time', t('log.table.time')),
      dataIndex: 'created_at',
      width: 170,
      render: (_, log) => renderTimestamp(log.created_at, log.request_id),
    },
    isAdminUser && {
      title: sortableTitle('channel', t('log.table.channel')),
      dataIndex: 'channel',
      width: 80,
      render: (value) =>
        value ? (
          <Link to={`/channel/edit/${value}`}>
            <Tag>{value}</Tag>
          </Link>
        ) : (
          ''
        ),
    },
    {
      title: sortableTitle('type', t('log.table.type')),
      dataIndex: 'type',
      width: 80,
      render: (value) => renderType(value),
    },
    {
      title: sortableTitle('model_name', t('log.table.model')),
      dataIndex: 'model_name',
      render: (value) => (value ? renderColorLabel(value) : ''),
    },
    showUserTokenQuota() &&
      isAdminUser && {
        title: sortableTitle('username', t('log.table.username')),
        dataIndex: 'username',
        render: (value, log) =>
          value ? (
            <Link to={`/user/edit/${log.user_id}`}>
              <Tag>{value}</Tag>
            </Link>
          ) : (
            ''
          ),
      },
    showUserTokenQuota() && {
      title: sortableTitle('token_name', t('log.table.token_name')),
      dataIndex: 'token_name',
      render: (value) => (value ? renderColorLabel(value) : ''),
    },
    showUserTokenQuota() && {
      title: sortableTitle('prompt_tokens', t('log.table.prompt_tokens')),
      dataIndex: 'prompt_tokens',
      width: 110,
      render: (value) => (value ? value : ''),
    },
    showUserTokenQuota() && {
      title: sortableTitle('completion_tokens', t('log.table.completion_tokens')),
      dataIndex: 'completion_tokens',
      width: 110,
      render: (value) => (value ? value : ''),
    },
    showUserTokenQuota() && {
      title: sortableTitle('quota', t('log.table.quota')),
      dataIndex: 'quota',
      width: 110,
      render: (value) => (value ? renderQuota(value, t, 6) : ''),
    },
    {
      title: t('log.table.detail'),
      dataIndex: 'content',
      render: (_, log) => renderDetail(log),
    },
  ].filter(Boolean);

  const pageLogs = logs
    .slice((activePage - 1) * ITEMS_PER_PAGE, activePage * ITEMS_PER_PAGE)
    .filter((log) => !log.deleted);

  return (
    <>
      <Typography.Title level={5} style={{ marginTop: 0 }}>
        {t('log.usage_details')}（{t('log.total_quota')}：
        {showStat && renderQuota(stat.quota, t)}
        {!showStat && (
          <span
            onClick={handleEyeClick}
            style={{ cursor: 'pointer', color: 'gray' }}
          >
            {t('log.click_to_view')}
          </span>
        )}
        ）
      </Typography.Title>
      <Space size={12} wrap style={{ marginBottom: 12 }}>
        <Input
          size='small'
          addonBefore={t('log.table.token_name')}
          value={token_name}
          placeholder={t('log.table.token_name_placeholder')}
          onChange={handleInputChange('token_name')}
          style={{ width: 180 }}
        />
        <Input
          size='small'
          addonBefore={t('log.table.model_name')}
          value={model_name}
          placeholder={t('log.table.model_name_placeholder')}
          onChange={handleInputChange('model_name')}
          style={{ width: 180 }}
        />
        <Input
          size='small'
          addonBefore={t('log.table.start_time')}
          value={start_timestamp}
          type='datetime-local'
          onChange={handleInputChange('start_timestamp')}
          style={{ width: 250 }}
        />
        <Input
          size='small'
          addonBefore={t('log.table.end_time')}
          value={end_timestamp}
          type='datetime-local'
          onChange={handleInputChange('end_timestamp')}
          style={{ width: 250 }}
        />
        <Button size='small' type='primary' onClick={refresh}>
          {t('log.buttons.submit')}
        </Button>
        {isAdminUser && (
          <>
            <Input
              size='small'
              addonBefore={t('log.table.channel_id')}
              value={channel}
              placeholder={t('log.table.channel_id_placeholder')}
              onChange={handleInputChange('channel')}
              style={{ width: 140 }}
            />
            <Input
              size='small'
              addonBefore={t('log.table.username')}
              value={username}
              placeholder={t('log.table.username_placeholder')}
              onChange={handleInputChange('username')}
              style={{ width: 160 }}
            />
          </>
        )}
        <Input
          allowClear
          size='small'
          prefix={<SearchOutlined />}
          placeholder={t('log.search')}
          value={searchKeyword}
          onChange={(e) => setSearchKeyword(e.target.value)}
          onPressEnter={searchLogs}
          style={{ width: 220 }}
        />
      </Space>
      <Table
        size='small'
        columns={columns}
        dataSource={pageLogs}
        rowKey='id'
        pagination={{
          current: activePage,
          pageSize: ITEMS_PER_PAGE,
          total: logs.length,
          showSizeChanger: false,
          onChange: onPaginationChange,
        }}
        footer={() => (
          <Space wrap>
            <Select
              placeholder={t('log.type.select')}
              options={LOG_OPTIONS}
              value={logType}
              style={{ minWidth: 120 }}
              onChange={(value) => setLogType(Number(value))}
            />
            <Button size='small' onClick={refresh} loading={loading}>
              {t('log.buttons.refresh')}
            </Button>
          </Space>
        )}
      />
    </>
  );
};

export default LogsTable;
