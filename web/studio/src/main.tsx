import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { Toaster } from 'sonner';

import App from './App';
import { UserProvider } from './context/User';
import './i18n';
import './index.css';

const root = ReactDOM.createRoot(document.getElementById('root')!);
root.render(
  <React.StrictMode>
    <UserProvider>
      <BrowserRouter>
        <App />
        <Toaster position='top-center' richColors />
      </BrowserRouter>
    </UserProvider>
  </React.StrictMode>
);
