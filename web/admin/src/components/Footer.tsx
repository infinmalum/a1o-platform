import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getFooterHTML, getSystemName } from '../helpers';

const Footer = () => {
  const { t } = useTranslation();
  const systemName = getSystemName();
  const [footer, setFooter] = useState(getFooterHTML());
  let remainCheckTimes = 5;

  const loadFooter = () => {
    let footer_html = localStorage.getItem('footer_html');
    if (footer_html) {
      setFooter(footer_html);
    }
  };

  useEffect(() => {
    const timer = setInterval(() => {
      if (remainCheckTimes <= 0) {
        clearInterval(timer);
        return;
      }
      remainCheckTimes--;
      loadFooter();
    }, 200);
    return () => clearInterval(timer);
  }, []);

  return (
    <footer className='custom-footer'>
      {footer ? (
        <div
          className='custom-footer-inner'
          dangerouslySetInnerHTML={{ __html: footer }}
        ></div>
      ) : (
        <div className='custom-footer-inner'>
          <a href='https://github.com/infinmalum/a1o-platform' target='_blank'>
            {systemName} {import.meta.env.REACT_APP_VERSION}{' '}
          </a>
          {t('footer.built_by')}{' '}
          <a href='https://github.com/songquanpeng/one-api' target='_blank'>
            {t('footer.built_by_name')}
          </a>{' '}
          {t('footer.license')}{' '}
          <a href='https://github.com/infinmalum/a1o-platform/blob/main/LICENSE'>
            {t('footer.licenseName')}
          </a>
        </div>
      )}
    </footer>
  );
};

export default Footer;
