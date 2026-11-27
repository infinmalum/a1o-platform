import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Input, Table, Typography } from 'antd';
import { SearchOutlined } from '@ant-design/icons';

import { API } from '../helpers';

interface PriceEditorProps {
  modelRatio: string;
  completionRatio: string;
  modelCachePrices: string;
  groupRatio: string;
  onChange: (patch: {
    ModelRatio?: string;
    CompletionRatio?: string;
    ModelCachePrices?: string;
    GroupRatio?: string;
  }) => void;
}

// Ratios are the internal unit: 1 means $0.002 per 1K tokens, i.e. $2 per
// million, scaled by QuotaPerUnit. Operators think in money, so the editor
// shows money and converts on the way in and out.
function quotaPerUnit(): number {
  const stored = parseFloat(localStorage.getItem('quota_per_unit') || '');
  return Number.isFinite(stored) && stored > 0 ? stored : 500000;
}

function ratioToPrice(ratio: number): number {
  return (ratio * 1e6) / quotaPerUnit();
}

function priceToRatio(price: number): number {
  return Number(((price * quotaPerUnit()) / 1e6).toFixed(6));
}

function parseMap<T = number>(json: string): Record<string, T> {
  try {
    const parsed = JSON.parse(json);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, T>;
    }
  } catch {
    // an empty or half-typed document simply shows no rows
  }
  return {};
}

function formatPrice(price: number): string {
  if (!Number.isFinite(price)) {
    return '';
  }
  return String(Number(price.toFixed(4)));
}

// PriceEditor replaces hand-edited ratio JSON with a price list. It shows the
// models the channels actually serve; searching pulls in any other model that
// already has a ratio so an operator can price it ahead of adding a channel.
const PriceEditor = ({
  modelRatio,
  completionRatio,
  modelCachePrices,
  groupRatio,
  onChange,
}: PriceEditorProps) => {
  const { t } = useTranslation();
  const [available, setAvailable] = useState<string[]>([]);
  // Ratios the server resolves for a model, which include the built-in defaults
  // the option JSON does not carry (most models have a completion ratio only
  // there). Without them the output price would silently show as 1x the input.
  const [served, setServed] = useState<
    Record<string, { input: number; output: number }>
  >({});
  const [keyword, setKeyword] = useState('');
  // In-progress text, so a half-typed "0." is not rewritten under the cursor.
  const [drafts, setDrafts] = useState<Record<string, string>>({});

  useEffect(() => {
    const loadModels = async () => {
      try {
        // The models this account's group can actually reach, which is the set
        // an operator is pricing; anything else is reachable through search.
        const res = await API.get('/api/user/available_models');
        const { success, data } = res.data;
        if (success && Array.isArray(data)) {
          setAvailable(data);
        }
      } catch {
        // the interceptor toasts the error
      }
    };
    const loadServedRatios = async () => {
      try {
        const res = await API.get('/api/user/pricing');
        const { success, data } = res.data;
        if (success && data && Array.isArray(data.models)) {
          const next: Record<string, { input: number; output: number }> = {};
          data.models.forEach((entry) => {
            if (entry && entry.priced) {
              next[entry.name] = {
                input: entry.model_ratio,
                output: entry.completion_ratio,
              };
            }
          });
          setServed(next);
        }
      } catch {
        // the interceptor toasts the error
      }
    };
    loadModels().then();
    loadServedRatios().then();
  }, []);

  const ratios = useMemo(() => parseMap(modelRatio), [modelRatio]);
  const completions = useMemo(() => parseMap(completionRatio), [completionRatio]);
  const cachePrices = useMemo(
    () => parseMap<Partial<Record<'read' | 'write' | 'write_1h', number>>>(modelCachePrices),
    [modelCachePrices]
  );
  const groups = useMemo(() => parseMap(groupRatio), [groupRatio]);

  const rows = useMemo(() => {
    const names = new Set(available);
    const trimmed = keyword.trim().toLowerCase();
    if (trimmed) {
      Object.keys(ratios).forEach((name) => {
        if (name.toLowerCase().includes(trimmed)) {
          names.add(name);
        }
      });
      if (trimmed) {
        names.add(keyword.trim());
      }
    }
    return Array.from(names).sort();
  }, [available, ratios, keyword]);

  // The option JSON wins when it has the model: it holds the operator's own
  // overrides, including edits made a moment ago in this form. The served value
  // fills in what only the server knows.
  const ratioOf = (model: string): number | undefined =>
    ratios[model] ?? served[model]?.input;

  const completionOf = (model: string): number | undefined =>
    completions[model] ?? served[model]?.output;

  const priceOf = (model: string): string => {
    const ratio = ratioOf(model);
    if (ratio === undefined) {
      return '';
    }
    return formatPrice(ratioToPrice(ratio));
  };

  const outputPriceOf = (model: string): string => {
    const ratio = ratioOf(model);
    const completion = completionOf(model);
    if (ratio === undefined || completion === undefined) {
      return '';
    }
    return formatPrice(ratioToPrice(ratio * completion));
  };

  // Dropping the draft lets the field fall back to the value derived from the
  // committed ratio, which is what the operator should see after an edit.
  const clearDraft = (key: string) => {
    setDrafts((current) => {
      const next = { ...current };
      delete next[key];
      return next;
    });
  };

  const commitInputPrice = (model: string, text: string) => {
    const price = parseFloat(text);
    if (!Number.isFinite(price) || price < 0) {
      clearDraft(`${model}:in`);
      return;
    }
    const next = { ...ratios, [model]: priceToRatio(price) };
    clearDraft(`${model}:in`);
    onChange({ ModelRatio: JSON.stringify(next, null, 2) });
  };

  const commitOutputPrice = (model: string, text: string) => {
    const price = parseFloat(text);
    const inputPrice = parseFloat(priceOf(model));
    if (!Number.isFinite(price) || price < 0 || !Number.isFinite(inputPrice) || inputPrice <= 0) {
      clearDraft(`${model}:out`);
      return;
    }
    const next = { ...completions, [model]: Number((price / inputPrice).toFixed(4)) };
    clearDraft(`${model}:out`);
    onChange({ CompletionRatio: JSON.stringify(next, null, 2) });
  };

  const commitCachePrice = (model: string, kind: 'read' | 'write' | 'write_1h', text: string) => {
    const price = Number(text);
    clearDraft(`${model}:cache-${kind}`);
    if (!Number.isFinite(price) || price < 0) {
      return;
    }
    const next = { ...cachePrices };
    const entry = { ...next[model] };
    if (text.trim() === '') {
      delete entry[kind];
    } else {
      entry[kind] = priceToRatio(price);
    }
    if (entry.read === undefined && entry.write === undefined && entry.write_1h === undefined) {
      delete next[model];
    } else {
      next[model] = entry;
    }
    onChange({ ModelCachePrices: JSON.stringify(next, null, 2) });
  };

  const commitGroupRatio = (group: string, text: string) => {
    const ratio = parseFloat(text);
    if (!Number.isFinite(ratio) || ratio <= 0) {
      clearDraft(`group:${group}`);
      return;
    }
    const next = { ...groups, [group]: ratio };
    clearDraft(`group:${group}`);
    onChange({ GroupRatio: JSON.stringify(next, null, 2) });
  };

  const value = (key: string, fallback: string) => drafts[key] ?? fallback;

  return (
    <>
      <Alert
        type='info'
        showIcon
        message={t('setting.operation.ratio.price.hint')}
        style={{ marginBottom: 12 }}
      />
      <Input
        allowClear
        prefix={<SearchOutlined />}
        placeholder={t('setting.operation.ratio.price.search')}
        value={keyword}
        onChange={(event) => setKeyword(event.target.value)}
        style={{ maxWidth: 360, marginBottom: 12 }}
      />
      <Table
        size='small'
        pagination={false}
        scroll={{ x: 760 }}
        dataSource={rows}
        rowKey={(model) => model}
        locale={{ emptyText: t('setting.operation.ratio.price.empty') }}
        columns={[
          {
            title: t('setting.operation.ratio.price.model'),
            dataIndex: 'model',
            render: (model) => model,
          },
          {
            title: t('setting.operation.ratio.price.input'),
            key: 'in',
            render: (_, model) => (
              <Input
                size='small'
                value={value(`${model}:in`, priceOf(model))}
                placeholder={t('setting.operation.ratio.price.unpriced')}
                onChange={(event) =>
                  setDrafts((current) => ({
                    ...current,
                    [`${model}:in`]: event.target.value,
                  }))
                }
                onBlur={(event) => commitInputPrice(model, event.target.value)}
              />
            ),
          },
          {
            title: t('setting.operation.ratio.price.output'),
            key: 'out',
            render: (_, model) => (
              <Input
                size='small'
                value={value(`${model}:out`, outputPriceOf(model))}
                placeholder={t('setting.operation.ratio.price.unpriced')}
                onChange={(event) =>
                  setDrafts((current) => ({
                    ...current,
                    [`${model}:out`]: event.target.value,
                  }))
                }
                onBlur={(event) => commitOutputPrice(model, event.target.value)}
              />
            ),
          },
          ...(['read', 'write', 'write_1h'] as const).map((kind) => ({
            title: t(`setting.operation.ratio.price.cache_${kind}`),
            key: `cache-${kind}`,
            render: (_: unknown, model: string) => {
              const key = `${model}:cache-${kind}`;
              const ratio = cachePrices[model]?.[kind];
              return (
                <Input
                  size='small'
                  aria-label={`${model} ${t(`setting.operation.ratio.price.cache_${kind}`)}`}
                  inputMode='decimal'
                  value={value(key, ratio === undefined ? '' : formatPrice(ratioToPrice(ratio)))}
                  placeholder={t(kind === 'write_1h' ? 'setting.operation.ratio.price.cache_write_rate' : 'setting.operation.ratio.price.cache_input_rate')}
                  onChange={(event) => {
                    const text = event.target.value;
                    setDrafts((current) => ({ ...current, [key]: text }));
                  }}
                  onBlur={(event) => {
                    if (drafts[key] !== undefined) {
                      commitCachePrice(model, kind, event.target.value);
                    }
                  }}
                />
              );
            },
          })),
        ]}
      />
      <Typography.Text type='secondary' style={{ fontSize: '0.85em' }}>
        {t('setting.operation.ratio.price.perMillion')}
      </Typography.Text>

      <Typography.Title level={5}>
        {t('setting.operation.ratio.price.groupTitle')}
      </Typography.Title>
      <Table
        size='small'
        pagination={false}
        dataSource={Object.keys(groups).sort()}
        rowKey={(group) => group}
        columns={[
          {
            title: t('setting.operation.ratio.price.group'),
            render: (group) => group,
          },
          {
            title: t('setting.operation.ratio.price.groupRatio'),
            render: (group) => (
              <Input
                size='small'
                value={value(`group:${group}`, String(groups[group]))}
                onChange={(event) =>
                  setDrafts((current) => ({
                    ...current,
                    [`group:${group}`]: event.target.value,
                  }))
                }
                onBlur={(event) => commitGroupRatio(group, event.target.value)}
              />
            ),
          },
        ]}
      />
    </>
  );
};

export default PriceEditor;
