import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Checkbox,
  Form,
  Grid,
  Header,
  Input,
  Modal,
  Segment,
  Tab,
  TextArea,
} from 'semantic-ui-react';
import { API, showSuccess } from '../helpers';
import { chatApi, type ChatMCPServer, type ChatSkill } from '../pages/Chat/chatApi';

// GlobalChatOptions edits the chat runtime's administrator options through
// the standard option endpoint.
const GlobalChatOptions: React.FC = () => {
  const { t } = useTranslation();
  const [sandbox, setSandbox] = React.useState(false);
  const [budget, setBudget] = React.useState(32000);
  const [maxIterations, setMaxIterations] = React.useState(8);
  const [maxFileBytes, setMaxFileBytes] = React.useState(0);
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
        setBudget(Number(options.ChatContextBudgetTokens) || 32000);
        setMaxIterations(Number(options.ChatMaxToolIterations) || 8);
        setMaxFileBytes(Number(options.ChatMaxFileBytes) || 0);
      } catch {
        // interceptor toasts the error
      }
    })();
  }, []);

  const save = async () => {
    setLoading(true);
    try {
      await API.put('/api/option/', { key: 'ChatSandboxEnabled', value: String(sandbox) });
      await API.put('/api/option/', { key: 'ChatContextBudgetTokens', value: String(budget) });
      await API.put('/api/option/', { key: 'ChatMaxToolIterations', value: String(maxIterations) });
      if (maxFileBytes > 0) {
        await API.put('/api/option/', { key: 'ChatMaxFileBytes', value: String(maxFileBytes) });
      }
      showSuccess(t('chat.saved'));
    } catch {
      // interceptor toasts the error
    } finally {
      setLoading(false);
    }
  };

  return (
    <Form loading={loading}>
      <Form.Field>
        <Checkbox
          toggle
          label={t('chat.sandboxToggle')}
          checked={sandbox}
          onChange={() => setSandbox(!sandbox)}
        />
      </Form.Field>
      <Form.Group widths='three'>
        <Form.Field>
          <label>{t('chat.contextBudget')}</label>
          <Input
            type='number'
            value={budget}
            onChange={(event, data) => setBudget(Number(data.value) || 0)}
          />
        </Form.Field>
        <Form.Field>
          <label>{t('chat.maxToolIterations')}</label>
          <Input
            type='number'
            value={maxIterations}
            onChange={(event, data) => setMaxIterations(Number(data.value) || 0)}
          />
        </Form.Field>
        <Form.Field>
          <label>{t('chat.maxFileBytes')}</label>
          <Input
            type='number'
            value={maxFileBytes}
            onChange={(event, data) => setMaxFileBytes(Number(data.value) || 0)}
          />
        </Form.Field>
      </Form.Group>
      <Button color='blue' onClick={save}>
        {t('chat.save')}
      </Button>
    </Form>
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
      setServers(await chatApi.adminListMCPServers());
    } catch {
      // interceptor toasts the error
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
      // interceptor toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.adminDeleteMCPServer(id);
      await reload();
    } catch {
      // interceptor toasts the error
    }
  };

  return (
    <div>
      <Button primary size='small' style={{ marginBottom: 10 }} onClick={() => startEdit({ ...emptyServer })}>
        {t('chat.newMCPServer')}
      </Button>
      {servers.map((server) => (
        <Segment key={server.id} style={{ display: 'flex', justifyContent: 'space-between' }}>
          <div>
            <strong>{server.name}</strong>{' '}
            <span style={{ color: '#999' }}>
              {server.transport === 'stdio' ? `${server.command} ${(server.args || []).join(' ')}` : server.url}
              {!server.enabled && ` · ${t('chat.disabled')}`}
            </span>
          </div>
          <div>
            <Button size='mini' basic onClick={() => startEdit(server)}>
              {t('chat.edit')}
            </Button>
            <Button size='mini' basic color='red' onClick={() => remove(server.id)}>
              {t('chat.delete')}
            </Button>
          </div>
        </Segment>
      ))}
      <Modal open={!!editing} onClose={() => setEditing(null)} size='small' closeIcon>
        <Modal.Header>{t('chat.editMCPServer')}</Modal.Header>
        <Modal.Content>
          {editing && (
            <Form>
              <Form.Field required>
                <label>{t('chat.mcpName')}</label>
                <Input value={editing.name} onChange={(event, data) => setEditing({ ...editing, name: String(data.value) })} />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.mcpTransport')}</label>
                <Button.Group>
                  <Button
                    basic={editing.transport !== 'http'}
                    color={editing.transport === 'http' ? 'blue' : undefined}
                    onClick={() => setEditing({ ...editing, transport: 'http' })}
                  >
                    HTTP
                  </Button>
                  <Button
                    basic={editing.transport !== 'stdio'}
                    color={editing.transport === 'stdio' ? 'blue' : undefined}
                    onClick={() => setEditing({ ...editing, transport: 'stdio' })}
                  >
                    stdio
                  </Button>
                </Button.Group>
              </Form.Field>
              {editing.transport === 'http' ? (
                <Form.Field required>
                  <label>URL</label>
                  <Input value={editing.url} onChange={(event, data) => setEditing({ ...editing, url: String(data.value) })} />
                </Form.Field>
              ) : (
                <>
                  <Form.Field required>
                    <label>{t('chat.mcpCommand')}</label>
                    <Input
                      placeholder='npx'
                      value={editing.command}
                      onChange={(event, data) => setEditing({ ...editing, command: String(data.value) })}
                    />
                  </Form.Field>
                  <Form.Field>
                    <label>{t('chat.mcpArgs')}</label>
                    <TextArea
                      value={argsText}
                      placeholder={'-y\n@modelcontextprotocol/server-filesystem\n/tmp'}
                      onChange={(event, data) => setArgsText(String(data.value ?? ''))}
                      style={{ minHeight: 80 }}
                    />
                  </Form.Field>
                </>
              )}
              <Form.Field>
                <label>{t('chat.mcpHeaders')}</label>
                <TextArea
                  value={headersText}
                  placeholder={'Authorization: Bearer xxx'}
                  onChange={(event, data) => setHeadersText(String(data.value ?? ''))}
                  style={{ minHeight: 70 }}
                />
              </Form.Field>
              <Form.Field>
                <Checkbox
                  toggle
                  label={t('chat.enabled')}
                  checked={editing.enabled}
                  onChange={() => setEditing({ ...editing, enabled: !editing.enabled })}
                />
              </Form.Field>
            </Form>
          )}
        </Modal.Content>
        <Modal.Actions>
          <Button onClick={() => setEditing(null)}>{t('chat.cancel')}</Button>
          <Button color='blue' onClick={save}>
            {t('chat.save')}
          </Button>
        </Modal.Actions>
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

  const reload = React.useCallback(async () => {
    try {
      setSkills(await chatApi.adminListSkills());
    } catch {
      // interceptor toasts the error
    }
  }, []);
  React.useEffect(() => {
    reload();
  }, [reload]);

  const startEdit = (skill: ChatSkill) => {
    setEditing({ ...skill });
    setToolText((skill.tools || []).join(', '));
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
      // interceptor toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.adminDeleteSkill(id);
      await reload();
    } catch {
      // interceptor toasts the error
    }
  };

  return (
    <div>
      <Button primary size='small' style={{ marginBottom: 10 }} onClick={() => startEdit({ ...emptySkill })}>
        {t('chat.newSkill')}
      </Button>
      {skills.map((skill) => (
        <Segment key={skill.id} style={{ display: 'flex', justifyContent: 'space-between' }}>
          <div>
            <strong>{skill.name}</strong>{' '}
            <span style={{ color: '#999' }}>
              {skill.description}
              {!skill.enabled && ` · ${t('chat.disabled')}`}
            </span>
          </div>
          <div>
            <Button size='mini' basic onClick={() => startEdit(skill)}>
              {t('chat.edit')}
            </Button>
            <Button size='mini' basic color='red' onClick={() => remove(skill.id)}>
              {t('chat.delete')}
            </Button>
          </div>
        </Segment>
      ))}
      <Modal open={!!editing} onClose={() => setEditing(null)} size='small' closeIcon>
        <Modal.Header>{t('chat.editSkill')}</Modal.Header>
        <Modal.Content>
          {editing && (
            <Form>
              <Form.Field required>
                <label>{t('chat.skillName')}</label>
                <Input value={editing.name} onChange={(event, data) => setEditing({ ...editing, name: String(data.value) })} />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.skillDescription')}</label>
                <Input
                  value={editing.description}
                  onChange={(event, data) => setEditing({ ...editing, description: String(data.value) })}
                />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.skillInstructions')}</label>
                <TextArea
                  value={editing.instructions}
                  onChange={(event, data) => setEditing({ ...editing, instructions: String(data.value ?? '') })}
                  style={{ minHeight: 120 }}
                />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.skillTools')}</label>
                <Input
                  value={toolText}
                  placeholder='sandbox, mcp_server_tool'
                  onChange={(event, data) => setToolText(String(data.value))}
                />
              </Form.Field>
              <Form.Field>
                <Checkbox
                  toggle
                  label={t('chat.enabled')}
                  checked={editing.enabled}
                  onChange={() => setEditing({ ...editing, enabled: !editing.enabled })}
                />
              </Form.Field>
            </Form>
          )}
        </Modal.Content>
        <Modal.Actions>
          <Button onClick={() => setEditing(null)}>{t('chat.cancel')}</Button>
          <Button color='blue' onClick={save}>
            {t('chat.save')}
          </Button>
        </Modal.Actions>
      </Modal>
    </div>
  );
};

const ChatAdminSetting: React.FC = () => {
  const { t } = useTranslation();
  return (
    <Grid stackable>
      <Grid.Row>
        <Grid.Column width={16}>
          <Header as='h4'>{t('chat.adminTitle')}</Header>
          <Tab
            panes={[
              { menuItem: t('chat.options'), render: () => <Tab.Pane><GlobalChatOptions /></Tab.Pane> },
              { menuItem: t('chat.globalMCPServers'), render: () => <Tab.Pane><GlobalMCPPane /></Tab.Pane> },
              { menuItem: t('chat.globalSkills'), render: () => <Tab.Pane><GlobalSkillPane /></Tab.Pane> },
            ]}
          />
        </Grid.Column>
      </Grid.Row>
    </Grid>
  );
};

export default ChatAdminSetting;
