import React from 'react';
import { Result } from 'antd';

const NotFound = () => (
  <Result
    status='404'
    title='页面不存在'
    subTitle='请检查你的浏览器地址是否正确'
  />
);

export default NotFound;
