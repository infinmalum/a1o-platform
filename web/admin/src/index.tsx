import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { useTranslation } from 'react-i18next';
import App from './App';
import Header from './components/Header';
import Footer from './components/Footer';
import './index.css';
import { UserProvider } from './context/User';
import { ToastContainer } from 'react-toastify';
import 'react-toastify/dist/ReactToastify.css';
import { StatusProvider } from './context/Status';
import './i18n';

const AntdLocaleProvider = ({ children }) => {
  const { i18n } = useTranslation();
  return (
    <ConfigProvider locale={i18n.language === 'zh' ? zhCN : enUS}>
      {children}
    </ConfigProvider>
  );
};

const root = ReactDOM.createRoot(document.getElementById('root'));
root.render(
  <React.StrictMode>
    <AntdLocaleProvider>
      <StatusProvider>
        <UserProvider>
          <BrowserRouter basename='/admin'>
            <Header />
            <main className={'main-content'}>
              <App />
            </main>
            <ToastContainer />
            <Footer />
          </BrowserRouter>
        </UserProvider>
      </StatusProvider>
    </AntdLocaleProvider>
  </React.StrictMode>
);
