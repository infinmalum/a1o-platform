import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Checkbox,
  Form,
  Input,
  Modal,
  Tab,
  TextArea,
} from 'semantic-ui-react';
import {
  chatApi,
  type ChatMCPServer,
  type ChatSkill,
} from './chatApi';

const emptySkill: ChatSkill = {
  id: 0,
  owner_id: -1,
  name: '',
  description: '',
  instructions: '',
  tools: null,
  enabled: true,
};

const SkillPane: React.FC = () => {
  const { t } = useTranslation();
  const [skills, setSkills] = React.useState<ChatSkill[]>([]);
  const [editing, setEditing] = React.useState<ChatSkill | null>(null);
  const [toolText, setToolText] = React.useState('');

  const reload = React.useCallback(async () => {
    try {
      const list = await chatApi.listSkills();
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

  const save = async () => {
    if (!editing) return;
    try {
      const tools = toolText
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      await chatApi.saveSkill({ ...editing, tools });
      setEditing(null);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.deleteSkill(id);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  return (
    <div>
      <Button
        primary
        size='small'
        onClick={() => startEdit({ ...emptySkill })}
        style={{ marginBottom: 10 }}
      >
        {t('chat.newSkill')}
      </Button>
      {skills.map((skill) => (
        <div
          key={skill.id}
          style={{
            border: '1px solid #e8e8e8',
            borderRadius: 6,
            padding: '8px 12px',
            marginBottom: 8,
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <strong>
              {skill.name}{' '}
              <span style={{ color: '#999', fontWeight: 400, fontSize: '0.8em' }}>
                {skill.owner_id === 0 ? t('chat.globalSkill') : t('chat.personalSkill')}
                {!skill.enabled && ` · ${t('chat.disabled')}`}
              </span>
            </strong>
            <span>
              {skill.owner_id !== 0 && (
                <>
                  <Button size='mini' basic onClick={() => startEdit(skill)}>
                    {t('chat.edit')}
                  </Button>
                  <Button size='mini' basic color='red' onClick={() => remove(skill.id)}>
                    {t('chat.delete')}
                  </Button>
                </>
              )}
            </span>
          </div>
          {skill.description && <div style={{ color: '#666' }}>{skill.description}</div>}
          {skill.instructions && (
            <div
              style={{
                whiteSpace: 'pre-wrap',
                fontSize: '0.85em',
                maxHeight: 80,
                overflow: 'hidden',
                color: '#444',
              }}
            >
              {skill.instructions}
            </div>
          )}
          {skill.tools && skill.tools.length > 0 && (
            <div style={{ marginTop: 4 }}>
              {skill.tools.map((tool) => (
                <span key={tool} style={{ fontSize: '0.8em', color: '#2185d0', marginRight: 8 }}>
                  {tool}
                </span>
              ))}
            </div>
          )}
        </div>
      ))}
      <Modal open={!!editing} onClose={() => setEditing(null)} size='small' closeIcon>
        <Modal.Header>{t('chat.editSkill')}</Modal.Header>
        <Modal.Content>
          {editing && (
            <Form>
              <Form.Field required>
                <label>{t('chat.skillName')}</label>
                <Input
                  value={editing.name}
                  onChange={(event, data) => setEditing({ ...editing, name: String(data.value) })}
                />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.skillDescription')}</label>
                <Input
                  value={editing.description}
                  onChange={(event, data) =>
                    setEditing({ ...editing, description: String(data.value) })
                  }
                />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.skillInstructions')}</label>
                <TextArea
                  value={editing.instructions}
                  onChange={(event, data) =>
                    setEditing({ ...editing, instructions: String(data.value ?? '') })
                  }
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

const emptyServer: ChatMCPServer = {
  id: 0,
  owner_id: -1,
  name: '',
  transport: 'http',
  command: '',
  args: null,
  env: null,
  url: '',
  headers: null,
  enabled: true,
};

const UserMCPPane: React.FC = () => {
  const { t } = useTranslation();
  const [servers, setServers] = React.useState<ChatMCPServer[]>([]);
  const [editing, setEditing] = React.useState<ChatMCPServer | null>(null);
  const [headersText, setHeadersText] = React.useState('');

  const reload = React.useCallback(async () => {
    try {
      const list = await chatApi.listUserMCPServers();
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
    setHeadersText(
      Object.entries(server.headers || {})
        .map(([key, value]) => `${key}: ${value}`)
        .join('\n')
    );
  };

  const save = async () => {
    if (!editing) return;
    try {
      const headers: Record<string, string> = {};
      headersText.split('\n').forEach((line) => {
        const index = line.indexOf(':');
        if (index > 0) {
          headers[line.slice(0, index).trim()] = line.slice(index + 1).trim();
        }
      });
      await chatApi.saveUserMCPServer({ ...editing, headers });
      setEditing(null);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  const remove = async (id: number) => {
    try {
      await chatApi.deleteUserMCPServer(id);
      await reload();
    } catch {
      // chatApi toasts the error
    }
  };

  return (
    <div>
      <Button
        primary
        size='small'
        onClick={() => startEdit({ ...emptyServer })}
        style={{ marginBottom: 10 }}
      >
        {t('chat.newMCPServer')}
      </Button>
      {servers.map((server) => (
        <div
          key={server.id}
          style={{
            border: '1px solid #e8e8e8',
            borderRadius: 6,
            padding: '8px 12px',
            marginBottom: 8,
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <strong>
              {server.name}{' '}
              <span style={{ color: '#999', fontWeight: 400, fontSize: '0.8em' }}>
                {server.url} · {!server.enabled && t('chat.disabled')}
              </span>
            </strong>
            <span>
              <Button size='mini' basic onClick={() => startEdit(server)}>
                {t('chat.edit')}
              </Button>
              <Button size='mini' basic color='red' onClick={() => remove(server.id)}>
                {t('chat.delete')}
              </Button>
            </span>
          </div>
        </div>
      ))}
      <Modal open={!!editing} onClose={() => setEditing(null)} size='small' closeIcon>
        <Modal.Header>{t('chat.editMCPServer')}</Modal.Header>
        <Modal.Content>
          {editing && (
            <Form>
              <Form.Field required>
                <label>{t('chat.mcpName')}</label>
                <Input
                  value={editing.name}
                  onChange={(event, data) => setEditing({ ...editing, name: String(data.value) })}
                />
              </Form.Field>
              <Form.Field required>
                <label>URL</label>
                <Input
                  placeholder='https://example.com/mcp'
                  value={editing.url}
                  onChange={(event, data) => setEditing({ ...editing, url: String(data.value) })}
                />
              </Form.Field>
              <Form.Field>
                <label>{t('chat.mcpHeaders')}</label>
                <TextArea
                  value={headersText}
                  placeholder={'Authorization: Bearer sk-xxx'}
                  onChange={(event, data) => setHeadersText(String(data.value ?? ''))}
                  style={{ minHeight: 80 }}
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

const ChatToolsModal: React.FC<{ open: boolean; onClose: () => void }> = ({ open, onClose }) => {
  const { t } = useTranslation();
  return (
    <Modal open={open} onClose={onClose} size='large' closeIcon>
      <Modal.Header>{t('chat.toolsManager')}</Modal.Header>
      <Modal.Content>
        <Tab
          panes={[
            { menuItem: t('chat.skills'), render: () => <Tab.Pane><SkillPane /></Tab.Pane> },
            { menuItem: t('chat.myMCPServers'), render: () => <Tab.Pane><UserMCPPane /></Tab.Pane> },
          ]}
        />
      </Modal.Content>
    </Modal>
  );
};

export default ChatToolsModal;
