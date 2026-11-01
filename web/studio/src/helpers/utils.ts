import { toast } from 'sonner';

export function isAdmin() {
  const user = localStorage.getItem('user');
  if (!user) return false;
  const role = (JSON.parse(user) as { role?: number }).role;
  return typeof role === 'number' && role >= 10;
}

export function getSystemName() {
  const system_name = localStorage.getItem('system_name');
  if (!system_name) return 'A1O Studio';
  return system_name;
}

export function getLogo() {
  const logo = localStorage.getItem('logo');
  if (!logo) return '/logo.png';
  return logo;
}

export function isMobile() {
  return window.innerWidth <= 600;
}

// Same failure mapping as the admin console: 401 bounces to the login page
// with the expired flag, other statuses become toasts.
export function showError(error: unknown) {
  if (!error) return;
  console.error(error);
  const err = error as { message?: string; name?: string; response?: { status: number } };
  if (err.message) {
    if (err.name === 'AxiosError' && err.response) {
      switch (err.response.status) {
        case 401:
          window.location.href = '/login?expired=true';
          break;
        case 429:
          toast.error('错误：请求次数过多，请稍后再试！');
          break;
        case 500:
          toast.error('错误：服务器内部错误，请联系管理员！');
          break;
        default:
          toast.error('错误：' + err.message);
      }
      return;
    }
    toast.error('错误：' + err.message);
  } else {
    toast.error('错误：' + err);
  }
}

export function showSuccess(message: string) {
  toast.success(message);
}

export function showInfo(message: string) {
  toast.info(message);
}
