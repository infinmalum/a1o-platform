import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from 'antd';
import { API, showError } from '../../helpers';
import { marked } from 'marked';

const About = () => {
  const { t } = useTranslation();
  const [about, setAbout] = useState('');
  const [aboutLoaded, setAboutLoaded] = useState(false);

  const displayAbout = async () => {
    setAbout(localStorage.getItem('about') || '');
    const res = await API.get('/api/about');
    const { success, message, data } = res.data;
    if (success) {
      let aboutContent = data;
      if (!data.startsWith('https://')) {
        aboutContent = marked.parse(data);
      }
      setAbout(aboutContent);
      localStorage.setItem('about', aboutContent);
    } else {
      showError(message);
      setAbout(t('about.loading_failed'));
    }
    setAboutLoaded(true);
  };

  useEffect(() => {
    displayAbout().then();
  }, []);

  return (
    <>
      {aboutLoaded && about === '' ? (
        <Card title={t('about.title')}>
          <p>{t('about.description')}</p>
          {t('about.repository')}
          <a href='https://github.com/infinmalum/a1o-platform'>
            https://github.com/infinmalum/a1o-platform
          </a>
        </Card>
      ) : about.startsWith('https://') ? (
        <iframe
          src={about}
          style={{ width: '100%', height: '100vh', border: 'none' }}
        />
      ) : (
        <Card>
          <div
            style={{ fontSize: 'larger' }}
            dangerouslySetInnerHTML={{ __html: about }}
          ></div>
        </Card>
      )}
    </>
  );
};

export default About;
