import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Form, Header, Input, Message, Table } from 'semantic-ui-react';

import { API } from '../helpers';

interface PriceEditorProps {
  modelRatio: string;
  completionRatio: string;
  groupRatio: string;
  onChange: (patch: {
    ModelRatio?: string;
    CompletionRatio?: string;
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

function parseMap(json: string): Record<string, number> {
  try {
    const parsed = JSON.parse(json);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, number>;
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
      <Message>{t('setting.operation.ratio.price.hint')}</Message>
      <Form.Input
        icon='search'
        placeholder={t('setting.operation.ratio.price.search')}
        value={keyword}
        onChange={(event) => setKeyword(event.target.value)}
      />
      <Table celled compact>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>
              {t('setting.operation.ratio.price.model')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('setting.operation.ratio.price.input')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('setting.operation.ratio.price.output')}
            </Table.HeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.map((model) => (
            <Table.Row key={model}>
              <Table.Cell>{model}</Table.Cell>
              <Table.Cell>
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
              </Table.Cell>
              <Table.Cell>
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
              </Table.Cell>
            </Table.Row>
          ))}
          {rows.length === 0 && (
            <Table.Row>
              <Table.Cell colSpan={3} style={{ color: '#999' }}>
                {t('setting.operation.ratio.price.empty')}
              </Table.Cell>
            </Table.Row>
          )}
        </Table.Body>
      </Table>
      <p style={{ color: '#999', fontSize: '0.85em' }}>
        {t('setting.operation.ratio.price.perMillion')}
      </p>

      <Header as='h4'>{t('setting.operation.ratio.price.groupTitle')}</Header>
      <Table celled compact>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>
              {t('setting.operation.ratio.price.group')}
            </Table.HeaderCell>
            <Table.HeaderCell>
              {t('setting.operation.ratio.price.groupRatio')}
            </Table.HeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {Object.keys(groups)
            .sort()
            .map((group) => (
              <Table.Row key={group}>
                <Table.Cell>{group}</Table.Cell>
                <Table.Cell>
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
                </Table.Cell>
              </Table.Row>
            ))}
        </Table.Body>
      </Table>
    </>
  );
};

export default PriceEditor;
