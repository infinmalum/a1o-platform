import React, { Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { DirectionProvider } from '@radix-ui/react-direction';
import { useTranslation } from 'react-i18next';
import { BrowserRouter } from 'react-router-dom';
import { Toaster } from 'sonner';

import App from './App';
import { UserProvider } from './context/User';
import './i18n';
import './index.css';

function Studio() {
  const { i18n } = useTranslation();
  const direction = i18n.dir(i18n.resolvedLanguage);
  return (
    <DirectionProvider dir={direction}>
      <UserProvider>
        <BrowserRouter>
          <App />
          <Toaster position='top-center' richColors dir={direction} />
        </BrowserRouter>
      </UserProvider>
    </DirectionProvider>
  );
}

const root = ReactDOM.createRoot(document.getElementById('root')!);
root.render(
  <React.StrictMode>
    <Suspense fallback={null}>
      <Studio />
    </Suspense>
  </React.StrictMode>
);
