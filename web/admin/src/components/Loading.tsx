import React from 'react';
import { Spin } from 'antd';

const Loading = ({ prompt: name = 'page' }) => {
  return (
    <div style={{ padding: '64px 0', textAlign: 'center' }}>
      <Spin size='large'>
        <div style={{ minHeight: 80, minWidth: 200 }} />
      </Spin>
      <div style={{ marginTop: 12, color: '#999' }}>加载{name}中...</div>
    </div>
  );
};

export default Loading;
