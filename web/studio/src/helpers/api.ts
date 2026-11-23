import axios from 'axios';

import { showError } from './utils';
import { expireSession } from './session';
import { requestError } from './studioRequest';

export const API = axios.create({
  baseURL: import.meta.env.REACT_APP_SERVER ? import.meta.env.REACT_APP_SERVER : '',
});

API.interceptors.response.use(
  (response) => {
    if (response.headers['x-studio-session-required'] === '1') {
      expireSession();
      return Promise.reject(requestError(401));
    }
    return response;
  },
  (error) => {
    if (axios.isCancel(error)) return Promise.reject(error);
    const status = error.response?.status;
    const path = error.config?.url ?? '';
    if (status === 401 && path !== '/api/user/login' && !path.startsWith('/api/oauth/')) {
      expireSession();
    } else {
      console.error('Studio request failed', path.split('?')[0], status);
      showError(requestError(status === 401 ? undefined : status));
    }
    return Promise.reject(error);
  }
);
