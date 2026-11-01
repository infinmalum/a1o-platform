import React, { useEffect, useState } from 'react';
import { Button, Form } from 'semantic-ui-react';
import { useTranslation } from 'react-i18next';
import {
  amountToQuota,
  formatAmount,
  quotaToAmount,
} from '../helpers/render';

// QuotaInput is the shared money field. The parent owns the raw integer
// quota; the input talks dollars by default so decimal amounts like $2.56
// become a whole quota number before they can reach the backend, with a
// raw-quota mode for pasting internal values. Text edits commit on blur,
// which fires before the click of any button the user moves to.
interface QuotaInputProps {
  label: string;
  name: string;
  quota: number | string;
  onQuotaChange: (name: string, value: number) => void;
  disabled?: boolean;
  placeholder?: string;
}

const QuotaInput = ({
  label,
  name,
  quota,
  onQuotaChange,
  disabled = false,
  placeholder = '',
}: QuotaInputProps) => {
  const { t } = useTranslation();
  const [usdMode, setUsdMode] = useState(true);
  const [text, setText] = useState('');

  const rawQuota = Math.round(Number(quota) || 0);
  const displayText = () =>
    usdMode ? formatAmount(quotaToAmount(rawQuota)) : String(rawQuota);

  useEffect(() => {
    setText(displayText());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [quota, usdMode]);

  const commit = () => {
    const num = parseFloat(text);
    if (!Number.isFinite(num)) {
      setText(displayText());
      return;
    }
    const next = usdMode ? amountToQuota(num) : Math.round(num);
    if (next !== rawQuota) {
      onQuotaChange(name, next);
    }
  };

  return (
    <Form.Field>
      <Form.Input
        label={
          usdMode
            ? t('common.quotaInput.usd_hint', {
                label,
                quota: rawQuota.toLocaleString(),
              })
            : t('common.quotaInput.quota_hint', {
                label,
                amount: quotaToAmount(rawQuota).toFixed(2),
              })
        }
        name={name}
        placeholder={placeholder}
        onChange={(e, { value }) => setText(value)}
        onBlur={commit}
        value={text}
        type='number'
        step={usdMode ? '0.01' : '1'}
        min='0'
        disabled={disabled}
        autoComplete='new-password'
        action={
          <Button
            type='button'
            tabIndex={-1}
            onClick={() => setUsdMode((mode) => !mode)}
          >
            {usdMode
              ? t('common.quotaInput.switch_quota')
              : t('common.quotaInput.switch_usd')}
          </Button>
        }
      />
    </Form.Field>
  );
};

export default QuotaInput;
