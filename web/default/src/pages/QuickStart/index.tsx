import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Divider,
  Form,
  Header,
  Icon,
  Label,
  Menu,
  Message,
  Table,
} from 'semantic-ui-react';
import { Link } from 'react-router-dom';

import { API, copy, showError, showSuccess } from '../../helpers';
import { renderQuota } from '../../helpers/render';

type Protocol = 'openaiChat' | 'openaiResponses' | 'anthropic' | 'gemini';

interface ModelPrice {
  name: string;
  priced: boolean;
  quota_per_million: number;
  output_quota_per_million: number;
}

const QuickStart = () => {
  const { t } = useTranslation();
  const [serverAddress, setServerAddress] = useState('');
  const [models, setModels] = useState<ModelPrice[]>([]);
  const [groupRatio, setGroupRatio] = useState(1);
  const [group, setGroup] = useState('default');
  const [keyword, setKeyword] = useState('');
  const [protocol, setProtocol] = useState<Protocol>('openaiChat');

  useEffect(() => {
    const storedStatus = localStorage.getItem('status');
    if (storedStatus) {
      const status = JSON.parse(storedStatus) as { server_address?: string };
      setServerAddress(status.server_address || window.location.origin);
    } else {
      setServerAddress(window.location.origin);
    }
    const loadPricing = async () => {
      try {
        const res = await API.get('/api/user/pricing');
        const { success, message, data } = res.data;
        if (success) {
          setModels(data.models || []);
          setGroupRatio(data.group_ratio ?? 1);
          setGroup(data.group || 'default');
        } else {
          showError(message);
        }
      } catch {
        // the interceptor already reported it
      }
    };
    loadPricing().then();
  }, []);

  const baseURL = `${serverAddress}/v1`;
  const exampleModel = models.find((model) => model.priced)?.name || 'gpt-4o';

  const snippets: Record<Protocol, { label: string; code: string }> = {
    openaiChat: {
      label: t('quickStart.protocols.openaiChat'),
      code: `curl ${baseURL}/chat/completions \\
  -H 'Content-Type: application/json' \\
  -H 'Authorization: Bearer YOUR_API_KEY' \\
  -d '{"model": "${exampleModel}", "messages": [{"role": "user", "content": "Hello"}]}'`,
    },
    openaiResponses: {
      label: t('quickStart.protocols.openaiResponses'),
      code: `curl ${baseURL}/responses \\
  -H 'Content-Type: application/json' \\
  -H 'Authorization: Bearer YOUR_API_KEY' \\
  -d '{"model": "${exampleModel}", "input": "Hello"}'`,
    },
    anthropic: {
      label: t('quickStart.protocols.anthropic'),
      code: `curl ${baseURL}/messages \\
  -H 'Content-Type: application/json' \\
  -H 'x-api-key: YOUR_API_KEY' \\
  -H 'anthropic-version: 2023-06-01' \\
  -d '{"model": "${exampleModel}", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}'`,
    },
    gemini: {
      label: t('quickStart.protocols.gemini'),
      code: `curl "${serverAddress}/v1beta/models/${exampleModel}:generateContent" \\
  -H 'Content-Type: application/json' \\
  -H 'x-goog-api-key: YOUR_API_KEY' \\
  -d '{"contents": [{"parts": [{"text": "Hello"}]}]}'`,
    },
  };

  const copyText = async (text: string) => {
    if (await copy(text)) {
      showSuccess(t('quickStart.copied'));
    } else {
      showError(t('quickStart.copyFailed'));
    }
  };

  const filtered = keyword
    ? models.filter((model) =>
        model.name.toLowerCase().includes(keyword.toLowerCase())
      )
    : models;

  return (
    <div className='dashboard-container'>
      <Card fluid className='chart-card'>
        <Card.Content>
          <Header as='h2'>{t('quickStart.title')}</Header>
          <p style={{ color: '#666' }}>{t('quickStart.subtitle')}</p>
        </Card.Content>
      </Card>

      <Card fluid className='chart-card' style={{ marginTop: '1em' }}>
        <Card.Content>
          <Header as='h3'>
            <Label circular color='blue' size='large'>
              1
            </Label>
            {t('quickStart.step1.title')}
          </Header>
          <p style={{ color: '#666' }}>{t('quickStart.step1.description')}</p>
          <Form.Input
            fluid
            readOnly
            action={{
              icon: 'copy',
              content: t('quickStart.copy'),
              onClick: () => copyText(baseURL),
            }}
            value={baseURL}
          />
        </Card.Content>
      </Card>

      <Card fluid className='chart-card' style={{ marginTop: '1em' }}>
        <Card.Content>
          <Header as='h3'>
            <Label circular color='blue' size='large'>
              2
            </Label>
            {t('quickStart.step2.title')}
          </Header>
          <p style={{ color: '#666' }}>{t('quickStart.step2.description')}</p>
          <Button as={Link} to='/token' primary icon labelPosition='left'>
            <Icon name='key' />
            {t('quickStart.step2.button')}
          </Button>
        </Card.Content>
      </Card>

      <Card fluid className='chart-card' style={{ marginTop: '1em' }}>
        <Card.Content>
          <Header as='h3'>
            <Label circular color='blue' size='large'>
              3
            </Label>
            {t('quickStart.step3.title')}
          </Header>
          <p style={{ color: '#666' }}>{t('quickStart.step3.description')}</p>
          <Menu tabular>
            {(Object.keys(snippets) as Protocol[]).map((key) => (
              <Menu.Item
                key={key}
                name={key}
                active={protocol === key}
                onClick={() => setProtocol(key)}
              >
                {snippets[key].label}
              </Menu.Item>
            ))}
          </Menu>
          <pre
            style={{
              background: '#1f2933',
              color: '#e4e7eb',
              padding: '1em',
              borderRadius: 8,
              overflowX: 'auto',
              fontFamily: 'JetBrains Mono, Consolas, monospace',
              fontSize: '0.85em',
              margin: '1em 0',
            }}
          >
            {snippets[protocol].code}
          </pre>
          <Button
            icon
            labelPosition='left'
            onClick={() => copyText(snippets[protocol].code)}
          >
            <Icon name='copy' />
            {t('quickStart.copy')}
          </Button>
          <Message info style={{ marginTop: '1em' }}>
            {t('quickStart.step3.clientHint')}
          </Message>
        </Card.Content>
      </Card>

      <Card fluid className='chart-card' style={{ marginTop: '1em' }}>
        <Card.Content>
          <Header as='h3'>{t('quickStart.pricing.title')}</Header>
          <p style={{ color: '#666' }}>
            {t('quickStart.pricing.description', { group })}
            {groupRatio !== 1 &&
              ` ${t('quickStart.pricing.groupRatio', { ratio: groupRatio })}`}
          </p>
          <Form.Input
            icon='search'
            placeholder={t('quickStart.pricing.search')}
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
          <Table celled compact>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>{t('quickStart.pricing.model')}</Table.HeaderCell>
                <Table.HeaderCell>
                  {t('quickStart.pricing.input')}
                </Table.HeaderCell>
                <Table.HeaderCell>
                  {t('quickStart.pricing.output')}
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((model) => (
                <Table.Row key={model.name}>
                  <Table.Cell>{model.name}</Table.Cell>
                  <Table.Cell>
                    {model.priced
                      ? renderQuota(model.quota_per_million, t)
                      : t('quickStart.pricing.unpriced')}
                  </Table.Cell>
                  <Table.Cell>
                    {model.priced
                      ? renderQuota(model.output_quota_per_million, t)
                      : t('quickStart.pricing.unpriced')}
                  </Table.Cell>
                </Table.Row>
              ))}
              {filtered.length === 0 && (
                <Table.Row>
                  <Table.Cell colSpan={3} textAlign='center'>
                    {t('quickStart.pricing.empty')}
                  </Table.Cell>
                </Table.Row>
              )}
            </Table.Body>
          </Table>
          <Label basic color='grey'>
            {t('quickStart.pricing.perMillion')}
          </Label>
          <Divider />
          <Link to='/dashboard'>{t('quickStart.pricing.usageLink')}</Link>
        </Card.Content>
      </Card>
    </div>
  );
};

export default QuickStart;
