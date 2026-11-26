import React, { useContext, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { UserContext } from '../context/User';
import { useTranslation } from 'react-i18next';
import { Button, Dropdown, Drawer, Menu, Space } from 'antd';
import {
  ApiOutlined,
  BarChartOutlined,
  ClusterOutlined,
  DollarOutlined,
  FileTextOutlined,
  GlobalOutlined,
  InfoCircleOutlined,
  LogoutOutlined,
  MenuOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons';
import { API, getLogo, getSystemName, isAdmin, isMobile, isRoot, showSuccess } from '../helpers';

// Operations console navigation: channel wiring, accounts, billing history and
// system configuration. End-user surfaces (chat, playgrounds, keys, top-up)
// live in the Studio app and are intentionally absent here.
let headerButtons: {
  name: string;
  to: string;
  icon: React.ReactNode;
  admin?: boolean;
  root?: boolean;
}[] = [
  { name: 'header.channel', to: '/channel', icon: <ClusterOutlined />, admin: true },
  { name: 'header.upstreamAccount', to: '/upstream_account', icon: <ApiOutlined />, admin: true },
  { name: 'header.redemption', to: '/redemption', icon: <DollarOutlined />, admin: true },
  { name: 'header.user', to: '/user', icon: <TeamOutlined />, admin: true },
  { name: 'header.dashboard', to: '/dashboard', icon: <BarChartOutlined />, admin: true },
  { name: 'header.log', to: '/log', icon: <FileTextOutlined /> },
  { name: 'header.setting', to: '/setting', icon: <SettingOutlined /> },
  { name: 'header.about', to: '/about', icon: <InfoCircleOutlined /> },
];

const languageOptions = [
  { key: 'zh', label: '中文' },
  { key: 'en', label: 'English' },
];

const Header = () => {
  const { t, i18n } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const navigate = useNavigate();
  const location = useLocation();

  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const systemName = getSystemName();
  const logo = getLogo();

  async function logout() {
    setMobileNavOpen(false);
    await API.get('/api/user/logout');
    showSuccess('注销成功!');
    userDispatch({ type: 'logout' });
    localStorage.removeItem('user');
    navigate('/login');
  }

  const visibleButtons = headerButtons.filter(
    (button) => !(button.admin && !isAdmin()) && !(button.root && !isRoot())
  );

  const menuItems = visibleButtons.map((button) => ({
    key: button.to,
    icon: button.icon,
    label: t(button.name),
  }));

  const changeLanguage = (language) => {
    i18n.changeLanguage(language);
  };

  const languageDropdown = (
    <Dropdown
      menu={{
        items: languageOptions.map((opt) => ({ key: opt.key, label: opt.label })),
        selectable: true,
        selectedKeys: [i18n.language],
        onClick: ({ key }) => changeLanguage(key),
      }}
      trigger={['click']}
    >
      <Button type='text' icon={<GlobalOutlined />} aria-label='language' />
    </Dropdown>
  );

  if (isMobile()) {
    return (
      <>
        <div className='admin-topbar'>
          <Link to='/' className='admin-topbar-brand'>
            <img src={logo} alt='logo' />
            <b>{systemName}</b>
          </Link>
          <Button
            type='text'
            icon={<MenuOutlined />}
            onClick={() => setMobileNavOpen(true)}
            aria-label='menu'
          />
        </div>
        <Drawer
          title={systemName}
          placement='right'
          open={mobileNavOpen}
          onClose={() => setMobileNavOpen(false)}
          width={260}
        >
          <Menu
            mode='inline'
            items={menuItems}
            selectedKeys={[location.pathname]}
            style={{ borderInlineEnd: 'none' }}
            onClick={({ key }) => {
              navigate(key);
              setMobileNavOpen(false);
            }}
          />
          <Space style={{ marginTop: 16, width: '100%' }} direction='vertical'>
            <div>{languageDropdown}</div>
            {userState.user ? (
              <Button block onClick={logout}>
                {t('header.logout')}
              </Button>
            ) : (
              <>
                <Button
                  block
                  onClick={() => {
                    setMobileNavOpen(false);
                    navigate('/login');
                  }}
                >
                  {t('header.login')}
                </Button>
                <Button
                  block
                  onClick={() => {
                    setMobileNavOpen(false);
                    navigate('/register');
                  }}
                >
                  {t('header.register')}
                </Button>
              </>
            )}
          </Space>
        </Drawer>
      </>
    );
  }

  return (
    <div className='admin-topbar'>
      <div className='container admin-topbar-inner'>
        <Link to='/' className='admin-topbar-brand hide-on-mobile'>
          <img src={logo} alt='logo' />
          <span className='admin-topbar-title'>{systemName}</span>
        </Link>
        <Menu
          mode='horizontal'
          items={menuItems}
          selectedKeys={[location.pathname]}
          style={{ flex: 1, minWidth: 0, borderBottom: 'none', background: 'transparent' }}
          onClick={({ key }) => navigate(key)}
        />
        <Space size={4}>
          {languageDropdown}
          {userState.user ? (
            <Dropdown
              menu={{
                items: [
                  {
                    key: 'logout',
                    icon: <LogoutOutlined />,
                    label: t('header.logout'),
                    onClick: logout,
                  },
                ],
              }}
              trigger={['click']}
            >
              <Button type='text'>{userState.user.username}</Button>
            </Dropdown>
          ) : (
            <Link to='/login' className='hide-on-mobile'>
              <Button type='text'>{t('header.login')}</Button>
            </Link>
          )}
        </Space>
      </div>
    </div>
  );
};

export default Header;
