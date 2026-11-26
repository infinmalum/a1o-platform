import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Checkbox,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tabs,
  Typography,
} from 'antd';
import { API, showError, showSuccess } from '../helpers';
import { chatApi, type ChatMCPServer, type ChatSkill } from '../helpers/chatApi';

const fieldStyle = { marginBottom: 16 };
const labelStyle = { display: 'block', marginBottom: 6, fontWeight: 500 };

// GlobalChatOptions edits the chat runtime's administrator options through
// the standard option endpoint.
const GlobalChatOptions: React.FC = () => {
  const { t } = useTranslation();
  const [sandbox, setSandbox] = React.useState(false);
  const [sandboxNetwork, setSandboxNetwork] = React.useState(false);
  const [budget, setBudget] = React.useState(32000);
  const [maxIterations, setMaxIterations] = React.useState(8);
  const [maxFileBytes, setMaxFileBytes] = React.useState(0);
  const [webTools, setWebTools] = React.useState(true);
  const [memory, setMemory] = React.useState(true);
  const [webSearchBackend, setWebSearchBackend] = React.useState('ddgs');
  const [webSearchAPIKey, setWebSearchAPIKey] = React.useState('');
  const [webSearchProxy, setWebSearchProxy] = React.useState('');
  const [workSubagent, setWorkSubagent] = React.useState(true);
  const [workSubagentMaxTurns, setWorkSubagentMaxTurns] = React.useState(12);
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    (async () => {
      try {
        const res = await API.get('/api/option/');
        const options: Record<string, string> = {};
        res.data.data?.forEach?.((item: { key: string; value: string }) => {
          options[item.key] = item.value;
        });
        setSandbox(options.ChatSandboxEnabled === 'true');
        setSandboxNetwork(options.ChatSandboxNetworkEnabled === 'true');
        setBudget(Number(options.ChatContextBudgetTokens) || 32000);
        setMaxIterations(Number(options.ChatMaxToolIterations) || 8);
        setMaxFileBytes(Number(options.ChatMaxFileBytes) || 0);
        setWebTools(options.ChatWebToolsEnabled !== 'false');
        setMemory(options.ChatMemoryEnabled !== 'false');
        setWebSearchBackend(options.ChatWebSearchBackend || 'ddgs');
        setWebSearchAPIKey(options.ChatWebSearchAPIKey || '');
        setWebSearchProxy(options.ChatWebSearchProxy || '');
        setWorkSubagent(options.WorkSubagentEnabled !== 'false');
        setWorkSubagentMaxTurns(Number(options.WorkSubagentMaxTurns) || 12);
      } catch {
        // interceptor toasts the error
      }
    })();
  }, []);

  const save = async () => {
    setLoading(true);
    try {
      await API.put('/api/option/', { key: 'ChatSandboxEnabled', value: String(sandbox) });
      await API.put('/api/option/', { key: 'ChatSandboxNetworkEnabled', value: String(sandboxNetwork) });
      await API.put('/api/option/', { key: 'ChatContextBudgetTokens', value: String(budget) });
      await API.put('/api/option/', { key: 'ChatMaxToolIterations', value: String(maxIterations) });
      if (maxFileBytes > 0) {
        await API.put('/api/option/', { key: 'ChatMaxFileBytes', value: String(maxFileBytes) });
      }
      await API.put('/api/option/', { key: 'ChatWebToolsEnabled', value: String(webTools) });
      await API.put('/api/option/', { key: 'ChatMemoryEnabled', value: String(memory) });
      await API.put('/api/option/', { key: 'ChatWebSearchBackend', value: webSearchBackend || 'ddgs' });
      await API.put('/api/option/', { key: 'ChatWebSearchAPIKey', value: webSearchAPIKey });
      await API.put('/api/option/', { key: 'ChatWebSearchProxy', value: webSearchProxy });
      await API.put('/api/option/', { key: 'WorkSubagentEnabled', value: String(workSubagent) });
      await API.put('/api/option/', { key: 'WorkSubagentMaxTurns', value: String(workSubagentMaxTurns) });
      showSuccess(t('chat.saved'));
    } catch {
      // interceptor toasts the error
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <Space size={24} wrap style={{ display: 'flex', marginBottom: 12 }}>
        <Checkbox checked={sandbox} onChange={() => setSandbox(!sandbox)}>
          {t('chat.sandboxToggle')}
        </Checkbox>
        <Checkbox
          checked={sandboxNetwork}
          onChange={() => setSandboxNetwork(!sandboxNetwork)}
        >
          {t('chat.sandboxNetworkToggle')}
        </Checkbox>
        <Checkbox checked={webTools} onChange={() => setWebTools(!webTools)}>
          {t('chat.webToolsToggle')}
        </Checkbox>
        <Checkbox checked={memory} onChange={() => setMemory(!memory)}>
          {t('chat.memoryToggle')}
        </Checkbox>
        <Checkbox
          checked={workSubagent}
          onChange={() => setWorkSubagent(!workSubagent)}
        >
          {t('chat.workSubagentToggle')}
        </Checkbox>
      </Space>
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 12 }}>
        <div>
          <label style={labelStyle}>{t('chat.contextBudget')}</label>
          <Input
            type='number'
            value={budget}
            style={{ width: 160 }}
            onChange={(event) => setBudget(Number(event.target.value) || 0)}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('chat.maxToolIterations')}</label>
          <Input
            type='number'
            value={maxIterations}
            style={{ width: 160 }}
            onChange={(event) => setMaxIterations(Number(event.target.value) || 0)}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('chat.maxFileBytes')}</label>
          <Input
            type='number'
            value={maxFileBytes}
            style={{ width: 160 }}
            onChange={(event) => setMaxFileBytes(Number(event.target.value) || 0)}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('chat.workSubagentMaxTurns')}</label>
          <Input
            type='number'
            value={workSubagentMaxTurns}
            style={{ width: 160 }}
            onChange={(event) =>
              setWorkSubagentMaxTurns(Number(event.target.value) || 0)
            }
          />
        </div>
      </Space>
      <Space size={24} wrap align='start' style={{ display: 'flex', marginBottom: 16 }}>
        <div>
          <label style={labelStyle}>{t('chat.webSearchBackend')}</label>
          <Select
            value={webSearchBackend}
            style={{ width: 200 }}
            options={[
              { key: 'ddgs', value: 'ddgs', label: t('chat.webSearchBackendDDGS') },
              { key: 'brave-free', value: 'brave-free', label: t('chat.webSearchBackendBrave') },
              { key: 'tavily', value: 'tavily', label: t('chat.webSearchBackendTavily') },
            ]}
            onChange={(value) => setWebSearchBackend(String(value ?? 'ddgs'))}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('chat.webSearchAPIKey')}</label>
          <Input.Password
            value={webSearchAPIKey}
            style={{ width: 280 }}
            onChange={(event) => setWebSearchAPIKey(String(event.target.value ?? ''))}
          />
        </div>
        <div>
          <label style={labelStyle}>{t('chat.webSearchProxy')}</label>
          <Input
            placeholder='http://127.0.0.1:7890'
            value={webSearchProxy}
            style={{ width: 280 }}
            onChange={(event) => setWebSearchProxy(String(event.target.value ?? ''))}
          />
        </div>
      </Space>
      <Button type='primary' loading={loading} onClick={save}>
        {t('chat.save')}
      </Button>
    </div>
  );
};

const emptyServer: ChatMCPServer = {
  id: 0,
  owner_id: 0,
  name: '',
  transport: 'http',
  command: '',
  args: [],
  env: {},
  url: '',
  headers: {},
  enabled: true,
};

const GlobalMCPPane: React.FC = () => {
  const { t } = useTranslation();
  const [servers, setServers] = React.useState<ChatMCPServer[]>([]);
  const [editing, setEditing] = React.useState<ChatMCPServer | null>(null);
  const [argsText, setArgsText] = React.useState('');
  const [headersText, setHeadersText] = React.useState('');

  const reload = React.useCallback(async () => {
    try {
      const list = await chatApi.adminListMCPServers();
      setServers(Array.isArray(list) ? list : []);
    } catch {
      // chatApi toasts the error
    }
  }, []);
  React.useEffect(() => {
    reload();
  }, [reload]);

  const startEdit = (server: ChatMCPServer) => {
    setEditing({ ...server });
    setArgsText((server.args || []).join('\n'));
    setHeadersText(
      Object.entries(server.headers || {})
        .map(([key, value]) => `${key}: ${value}`)
        .join('\n')
    );
  };

  const save = async () => {
    if (!editing) return;
    try {
      const args = argsText.split('\n').map((item) => item.trim()).filter(Boolean);
      const headers: Record<string, string> = {};
      headersText.split('\n').forEach((line) => {
        const index = line.indexOf(':');
        if (index > 0) {
          headers[line.slice(0, index).trim()] = line.slice(index + 1).trim();
        }
      });
      await chatApi.adminSaveMCPServer({ ...editing, args, headers });
      showSuccess(t('chat.saved'));
      setEditing(null);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.adminDeleteMCPServer(id);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  return (
    <div>
      <Button
        type='primary'
        size='small'
        style={{ marginBottom: 10 }}
        onClick={() => startEdit({ ...emptyServer })}
      >
        {t('chat.newMCPServer')}
      </Button>
      {servers.map((server) => (
        <Card
          key={server.id}
          size='small'
          style={{ marginBottom: 10 }}
          styles={{ body: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } }}
        >
          <div>
            <strong>{server.name}</strong>{' '}
            <span style={{ color: '#999' }}>
              {server.transport === 'stdio' ? `${server.command} ${(server.args || []).join(' ')}` : server.url}
              {!server.enabled && ` · ${t('chat.disabled')}`}
            </span>
          </div>
          <Space size={8}>
            <Button size='small' onClick={() => startEdit(server)}>
              {t('chat.edit')}
            </Button>
            <Popconfirm
              title={t('chat.delete')}
              onConfirm={() => remove(server.id)}
            >
              <Button size='small' danger>
                {t('chat.delete')}
              </Button>
            </Popconfirm>
          </Space>
        </Card>
      ))}
      <Modal
        open={!!editing}
        onCancel={() => setEditing(null)}
        title={t('chat.editMCPServer')}
        footer={[
          <Button key='cancel' onClick={() => setEditing(null)}>
            {t('chat.cancel')}
          </Button>,
          <Button key='save' type='primary' onClick={save}>
            {t('chat.save')}
          </Button>,
        ]}
      >
        {editing && (
          <div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('chat.mcpName')} <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                value={editing.name}
                onChange={(event) =>
                  setEditing({ ...editing, name: event.target.value })
                }
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('chat.mcpTransport')}</label>
              <Space.Compact>
                <Button
                  type={editing.transport === 'http' ? 'primary' : 'default'}
                  onClick={() => setEditing({ ...editing, transport: 'http' })}
                >
                  HTTP
                </Button>
                <Button
                  type={editing.transport === 'stdio' ? 'primary' : 'default'}
                  onClick={() => setEditing({ ...editing, transport: 'stdio' })}
                >
                  stdio
                </Button>
              </Space.Compact>
            </div>
            {editing.transport === 'http' ? (
              <div style={fieldStyle}>
                <label style={labelStyle}>
                  URL <span style={{ color: '#ff4d4f' }}>*</span>
                </label>
                <Input
                  value={editing.url}
                  onChange={(event) =>
                    setEditing({ ...editing, url: event.target.value })
                  }
                />
              </div>
            ) : (
              <>
                <div style={fieldStyle}>
                  <label style={labelStyle}>
                    {t('chat.mcpCommand')}{' '}
                    <span style={{ color: '#ff4d4f' }}>*</span>
                  </label>
                  <Input
                    placeholder='npx'
                    value={editing.command}
                    onChange={(event) =>
                      setEditing({ ...editing, command: event.target.value })
                    }
                  />
                </div>
                <div style={fieldStyle}>
                  <label style={labelStyle}>{t('chat.mcpArgs')}</label>
                  <Input.TextArea
                    value={argsText}
                    placeholder={'-y\n@modelcontextprotocol/server-filesystem\n/tmp'}
                    onChange={(event) => setArgsText(event.target.value)}
                    rows={4}
                  />
                </div>
              </>
            )}
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('chat.mcpHeaders')}</label>
              <Input.TextArea
                value={headersText}
                placeholder={'Authorization: Bearer xxx'}
                onChange={(event) => setHeadersText(event.target.value)}
                rows={3}
              />
            </div>
            <Checkbox
              checked={editing.enabled}
              onChange={() => setEditing({ ...editing, enabled: !editing.enabled })}
            >
              {t('chat.enabled')}
            </Checkbox>
          </div>
        )}
      </Modal>
    </div>
  );
};

const emptySkill: ChatSkill = {
  id: 0,
  owner_id: 0,
  name: '',
  description: '',
  instructions: '',
  tools: null,
  enabled: true,
};

const GlobalSkillPane: React.FC = () => {
  const { t } = useTranslation();
  const [skills, setSkills] = React.useState<ChatSkill[]>([]);
  const [editing, setEditing] = React.useState<ChatSkill | null>(null);
  const [toolText, setToolText] = React.useState('');
  const [importing, setImporting] = React.useState(false);

  const reload = React.useCallback(async () => {
    try {
      const list = await chatApi.adminListSkills();
      setSkills(Array.isArray(list) ? list : []);
    } catch {
      // chatApi toasts the error
    }
  }, []);
  React.useEffect(() => {
    reload();
  }, [reload]);

  const startEdit = (skill: ChatSkill) => {
    setEditing({ ...skill });
    setToolText((skill.tools || []).join(', '));
  };

  const importMarkdown = async (file: File) => {
    if (file.size > 64 * 1024) {
      showError(t('chat.skillMarkdownTooLarge'));
      return;
    }
    setImporting(true);
    try {
      const draft = await chatApi.previewSkillMarkdown(file);
      setEditing((current) => current && current === editing ? { ...current, ...draft } : current);
    } catch {
      // chatApi displays the error.
    } finally {
      setImporting(false);
    }
  };

  const save = async () => {
    if (!editing) return;
    try {
      const tools = toolText.split(',').map((item) => item.trim()).filter(Boolean);
      await chatApi.adminSaveSkill({ ...editing, tools });
      showSuccess(t('chat.saved'));
      setEditing(null);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.adminDeleteSkill(id);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  return (
    <div>
      <Button
        type='primary'
        size='small'
        style={{ marginBottom: 10 }}
        onClick={() => startEdit({ ...emptySkill })}
      >
        {t('chat.newSkill')}
      </Button>
      {skills.map((skill) => (
        <Card
          key={skill.id}
          size='small'
          style={{ marginBottom: 10 }}
          styles={{ body: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' } }}
        >
          <div>
            <strong>{skill.name}</strong>{' '}
            <span style={{ color: '#999' }}>
              {skill.description}
              {!skill.enabled && ` · ${t('chat.disabled')}`}
            </span>
          </div>
          <Space size={8}>
            <Button size='small' onClick={() => startEdit(skill)}>
              {t('chat.edit')}
            </Button>
            <Popconfirm
              title={t('chat.delete')}
              onConfirm={() => remove(skill.id)}
            >
              <Button size='small' danger>
                {t('chat.delete')}
              </Button>
            </Popconfirm>
          </Space>
        </Card>
      ))}
      <Modal
        open={!!editing}
        onCancel={() => setEditing(null)}
        title={t('chat.editSkill')}
        footer={[
          <Button key='cancel' onClick={() => setEditing(null)}>
            {t('chat.cancel')}
          </Button>,
          <Button key='save' type='primary' disabled={importing} onClick={save}>
            {t('chat.save')}
          </Button>,
        ]}
      >
        {editing && (
          <div>
            <div style={fieldStyle}>
              <label htmlFor='global-skill-markdown' style={labelStyle}>
                {t('chat.importSkillMarkdown')}
              </label>
              <Input
                id='global-skill-markdown'
                type='file'
                accept='.md,text/markdown'
                disabled={importing}
                onChange={(event) => {
                  const file = event.currentTarget.files?.[0];
                  event.currentTarget.value = '';
                  if (file) void importMarkdown(file);
                }}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>
                {t('chat.skillName')} <span style={{ color: '#ff4d4f' }}>*</span>
              </label>
              <Input
                value={editing.name}
                onChange={(event) =>
                  setEditing({ ...editing, name: event.target.value })
                }
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('chat.skillDescription')}</label>
              <Input
                value={editing.description}
                onChange={(event) =>
                  setEditing({ ...editing, description: event.target.value })
                }
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('chat.skillInstructions')}</label>
              <Input.TextArea
                value={editing.instructions}
                onChange={(event) =>
                  setEditing({ ...editing, instructions: event.target.value })
                }
                rows={6}
              />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>{t('chat.skillTools')}</label>
              <Input
                value={toolText}
                placeholder='sandbox, mcp_server_tool'
                onChange={(event) => setToolText(event.target.value)}
              />
            </div>
            <Checkbox
              checked={editing.enabled}
              onChange={() => setEditing({ ...editing, enabled: !editing.enabled })}
            >
              {t('chat.enabled')}
            </Checkbox>
          </div>
        )}
      </Modal>
    </div>
  );
};

const ChatAdminSetting: React.FC = () => {
  const { t } = useTranslation();
  return (
    <div>
      <Typography.Title level={5}>{t('chat.adminTitle')}</Typography.Title>
      <Tabs
        items={[
          { key: 'options', label: t('chat.options'), children: <GlobalChatOptions /> },
          { key: 'mcp', label: t('chat.globalMCPServers'), children: <GlobalMCPPane /> },
          { key: 'skills', label: t('chat.globalSkills'), children: <GlobalSkillPane /> },
        ]}
      />
    </div>
  );
};

export default ChatAdminSetting;
