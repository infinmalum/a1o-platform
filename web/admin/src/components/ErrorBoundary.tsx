import React from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Result, Typography } from 'antd';

// The fallback renders through a function component so it can translate;
// the boundary itself stays a class, which React requires.
const Fallback: React.FC<{ error: Error | null }> = ({ error }) => {
  const { t } = useTranslation();
  return (
    <Result
      status='error'
      title={t('errorBoundary.title')}
      subTitle={
        <Typography.Text type='secondary'>
          {error?.message || String(error)}
        </Typography.Text>
      }
      extra={
        <Button type='primary' onClick={() => window.location.reload()}>
          {t('errorBoundary.reload')}
        </Button>
      }
    />
  );
};

// ErrorBoundary keeps one broken page from blanking the whole console: a
// render error anywhere below it degrades to a reload card.
class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('render error', error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return <Fallback error={this.state.error} />;
    }
    return this.props.children;
  }
}

export default ErrorBoundary;
