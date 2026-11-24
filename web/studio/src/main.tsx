import React, { Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { DirectionProvider } from '@radix-ui/react-direction';
import { useTranslation } from 'react-i18next';
import { BrowserRouter } from 'react-router-dom';
import { Toaster } from 'sonner';

import App from './App';
import { UserProvider } from './context/User';
import { loadStudioStatus, type StudioStatus } from './helpers/studioStatus';
import { getSystemName } from './helpers/utils';
import { initializeStudioLanguage } from './i18n';
import './index.css';

function Studio({ status }: { status: StudioStatus | null }) {
  const { i18n } = useTranslation();
  const direction = i18n.dir(i18n.resolvedLanguage);
  return (
    <DirectionProvider dir={direction}>
      <UserProvider>
        <BrowserRouter>
          <App status={status} />
          <Toaster position='top-center' richColors dir={direction} />
        </BrowserRouter>
      </UserProvider>
    </DirectionProvider>
  );
}

async function startStudio() {
  const status = await loadStudioStatus();
  if (status) {
    localStorage.setItem('status', JSON.stringify(status));
    if (status.system_name) localStorage.setItem('system_name', status.system_name);
    if (status.logo) localStorage.setItem('logo', status.logo);
    if (status.quota_per_unit !== undefined) localStorage.setItem('quota_per_unit', String(status.quota_per_unit));
    if (status.display_in_currency !== undefined) localStorage.setItem('display_in_currency', String(status.display_in_currency));
    document.title = getSystemName();
    const link = document.querySelector<HTMLLinkElement>("link[rel~='icon']");
    if (link && status.logo) link.href = status.logo;
  }
  await initializeStudioLanguage(status);

  const root = ReactDOM.createRoot(document.getElementById('root')!);
  root.render(
    <React.StrictMode>
      <Suspense fallback={null}>
        <Studio status={status} />
      </Suspense>
    </React.StrictMode>
  );
}

void startStudio();
