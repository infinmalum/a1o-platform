import React from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Confirm, Dropdown, Icon, Input, List, Modal } from 'semantic-ui-react';
import ChatMessageList, { type DisplayMessage } from './ChatMessageList';
import ChatComposer from './ChatComposer';
import ChatSettingsModal, { type ChatSettingsValue } from './ChatSettingsModal';
import ChatToolsModal from './ChatToolsModal';
import { API } from '../../helpers';
import { renderQuota } from '../../helpers/render';
import {
  chatApi,
  runTurn,
  type ChatBootstrap,
  type ChatEvent,
  type ChatFile,
  type ChatSession,
  type ChatSkill,
} from './chatApi';

const Chat = () => {
  const { t } = useTranslation();
  const [sessions, setSessions] = React.useState<ChatSession[]>([]);
  const [activeSession, setActiveSession] = React.useState<ChatSession | null>(null);
  const [messages, setMessages] = React.useState<DisplayMessage[]>([]);
  const [input, setInput] = React.useState('');
  const [files, setFiles] = React.useState<ChatFile[]>([]);
  const [selectedFileIds, setSelectedFileIds] = React.useState<number[]>([]);
  const [skills, setSkills] = React.useState<ChatSkill[]>([]);
  const [selectedSkillIds, setSelectedSkillIds] = React.useState<number[]>([]);
  const [models, setModels] = React.useState<string[]>([]);
  // The model for the next created session; existing sessions carry their
  // own model, which the header picker rewrites directly.
  const [defaultModel, setDefaultModel] = React.useState('');
  const [bootstrap, setBootstrap] = React.useState<ChatBootstrap | null>(null);
  const [streaming, setStreaming] = React.useState(false);
  const [streamError, setStreamError] = React.useState('');
  const [settingsOpen, setSettingsOpen] = React.useState(false);
  const [toolsOpen, setToolsOpen] = React.useState(false);
  const [renameTarget, setRenameTarget] = React.useState<ChatSession | null>(null);
  const [renameValue, setRenameValue] = React.useState('');
  const [deleteTarget, setDeleteTarget] = React.useState<ChatSession | null>(null);
  const [quota, setQuota] = React.useState(0);
  const abortRef = React.useRef<AbortController | null>(null);

  // Chat spends the same balance as the API, so show it where the user is
  // rather than making them open a separate page to find out.
  React.useEffect(() => {
    const loadQuota = async () => {
      try {
        const res = await API.get('/api/user/self');
        const { success, data } = res.data;
        if (success && data) {
          setQuota(data.quota ?? 0);
        }
      } catch {
        // the interceptor toasts the error
      }
    };
    loadQuota().then();
  }, []);

  const reloadSessions = React.useCallback(async () => {
    try {
      setSessions(await chatApi.listSessions());
    } catch {
      // chatApi toasts the error
    }
  }, []);

  const loadMessages = React.useCallback(async (sessionId: number) => {
    const rows = await chatApi.listMessages(sessionId);
    return rows.map((row) => ({
      id: row.id,
      role: row.role,
      content: row.content,
      toolResult:
        row.role === 'tool'
          ? { id: row.tool_call_id, name: row.name, content: row.content, isError: false }
          : undefined,
    }));
  }, []);

  const openSession = React.useCallback(
    async (session: ChatSession) => {
      setActiveSession(session);
      setSelectedSkillIds(session.skill_ids || []);
      setSelectedFileIds([]);
      setStreamError('');
      try {
        setMessages(await loadMessages(session.id));
      } catch {
        // chatApi toasts the error
      }
    },
    [loadMessages]
  );

  React.useEffect(() => {
    reloadSessions();
    chatApi
      .bootstrap()
      .then(setBootstrap)
      .catch(() => undefined);
    chatApi
      .listFiles()
      .then(setFiles)
      .catch(() => undefined);
    chatApi
      .listSkills()
      .then(setSkills)
      .catch(() => undefined);
    chatApi
      .listModels()
      .then(setModels)
      .catch(() => undefined);
  }, [reloadSessions]);

  const createSession = async () => {
    const fallback = defaultModel || models[0] || 'gpt-4o-mini';
    try {
      const session = await chatApi.createSession({ model: fallback });
      await reloadSessions();
      setActiveSession(session);
      setMessages([]);
      setSelectedSkillIds([]);
      setSelectedFileIds([]);
    } catch {
      // chatApi toasts the error
    }
  };

  const toggleFile = (id: number) => {
    setSelectedFileIds((current) =>
      current.includes(id) ? current.filter((item) => item !== id) : [...current, id]
    );
  };

  const toggleSkill = (id: number) => {
    setSelectedSkillIds((current) =>
      current.includes(id) ? current.filter((item) => item !== id) : [...current, id]
    );
  };

  const uploadFile = async (file: File) => {
    try {
      const stored = await chatApi.uploadFile(file);
      setFiles((current) => [stored, ...current]);
      setSelectedFileIds((current) => [...current, stored.id]);
    } catch {
      // chatApi toasts the error
    }
  };

  const saveSettings = async (value: ChatSettingsValue) => {
    if (!activeSession) return;
    // The update endpoint validates the full payload, so submit the merged
    // session rather than a sparse patch.
    const updated = await chatApi.updateSession(activeSession.id, {
      ...activeSession,
      ...value,
    });
    setActiveSession(updated);
    setSelectedSkillIds(value.skill_ids);
    await reloadSessions();
  };

  const send = async () => {
    if (!activeSession || streaming) {
      return;
    }
    const content = input.trim();
    if (!content && selectedFileIds.length === 0) {
      return;
    }
    const attachments = selectedFileIds
      .map((id) => files.find((file) => file.id === id))
      .filter((file): file is ChatFile => !!file);
    const optimisticId = -Date.now();
    setMessages((current) => [
      ...current,
      {
        id: optimisticId,
        role: 'user',
        content,
        attachments: attachments.map((file) => ({ id: file.id, name: file.name, kind: file.kind })),
      },
    ]);
    setInput('');
    setSelectedFileIds([]);
    setStreaming(true);
    setStreamError('');
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await runTurn(activeSession.id, content, selectedFileIds, handleEvent, controller.signal);
    } catch (error) {
      if (!controller.signal.aborted) {
        setStreamError(error instanceof Error ? error.message : String(error));
      }
    } finally {
      setStreaming(false);
      abortRef.current = null;
      // Reload persisted history: the server owns the tool loop's
      // intermediate rows.
      try {
        setMessages(await loadMessages(activeSession.id));
      } catch {
        // keep the optimistic display
      }
      await reloadSessions();
      chatApi
        .listSkills()
        .then(setSkills)
        .catch(() => undefined);
    }
  };

  const handleEvent = (event: ChatEvent) => {
    setMessages((current) => {
      const next = [...current];
      const last = next[next.length - 1];
      switch (event.type) {
        case 'message_start':
          next.push({
            id: event.message_id || -Date.now(),
            role: 'assistant',
            content: '',
            streaming: true,
          });
          break;
        case 'delta':
          if (last && last.role === 'assistant' && last.streaming) {
            next[next.length - 1] = { ...last, content: last.content + (event.delta || '') };
          }
          break;
        case 'tool_call':
          if (last && last.role === 'assistant') {
            next[next.length - 1] = {
              ...last,
              streaming: false,
              toolCalls: [
                ...(last.toolCalls || []),
                {
                  id: event.tool_call_id || '',
                  name: event.name || '',
                  arguments: event.arguments || '',
                },
              ],
            };
          }
          break;
        case 'tool_result':
          next.push({
            id: -(Date.now() + next.length),
            role: 'tool',
            content: event.content || '',
            toolResult: {
              id: event.tool_call_id || '',
              name: event.name || '',
              content: event.content || '',
              isError: !!event.is_error,
            },
          });
          break;
        case 'compact':
          next.push({
            id: -(Date.now() + next.length),
            role: 'assistant',
            content: '',
            compacted: true,
          });
          break;
        case 'error':
          setStreamError(event.error || t('chat.requestFailed'));
          if (last && last.role === 'assistant') {
            next[next.length - 1] = { ...last, streaming: false };
          }
          break;
        case 'done':
          if (last && last.role === 'assistant') {
            next[next.length - 1] = { ...last, streaming: false };
          }
          break;
        default:
          break;
      }
      return next;
    });
  };

  const changeModel = async (model: string) => {
    if (!model) return;
    if (!activeSession) {
      setDefaultModel(model);
      return;
    }
    try {
      // The turn runner reads the session model fresh from the database, so
      // rewriting it here takes effect on the very next message.
      const updated = await chatApi.updateSession(activeSession.id, {
        ...activeSession,
        model,
      });
      setActiveSession(updated);
      await reloadSessions();
    } catch {
      // chatApi toasts the error
    }
  };

  const stop = () => {
    abortRef.current?.abort();
  };

  const rename = async () => {
    if (!renameTarget) return;
    try {
      await chatApi.updateSession(renameTarget.id, {
        ...renameTarget,
        title: renameValue,
        skill_ids: renameTarget.skill_ids || [],
      });
      await reloadSessions();
      if (activeSession?.id === renameTarget.id) {
        setActiveSession({ ...activeSession, title: renameValue });
      }
    } catch {
      // chatApi toasts the error
    }
    setRenameTarget(null);
  };

  const removeSession = async () => {
    if (!deleteTarget) return;
    try {
      await chatApi.deleteSession(deleteTarget.id);
      if (activeSession?.id === deleteTarget.id) {
        setActiveSession(null);
        setMessages([]);
      }
      await reloadSessions();
    } catch {
      // chatApi toasts the error
    }
    setDeleteTarget(null);
  };

  return (
    <div className='dashboard-container'>
      <div
        style={{
          display: 'flex',
          height: 'calc(100vh - 62px)',
          minHeight: 480,
          border: '1px solid #e8e8e8',
          borderRadius: 8,
          overflow: 'hidden',
          background: '#fff',
        }}
      >
        <div
          style={{
            width: 260,
            borderRight: '1px solid #e8e8e8',
            display: 'flex',
            flexDirection: 'column',
            background: '#fafafa',
          }}
        >
          <div style={{ padding: 10 }}>
            <Button fluid primary onClick={createSession}>
              <Icon name='plus' /> {t('chat.newChat')}
            </Button>
          </div>
          <div style={{ flex: 1, overflowY: 'auto' }}>
            <List selection>
              {sessions.map((session) => (
                <List.Item
                  key={session.id}
                  active={activeSession?.id === session.id}
                  onClick={() => openSession(session)}
                  style={{
                    padding: '10px 12px',
                    background: activeSession?.id === session.id ? '#e8f0fe' : undefined,
                  }}
                >
                  <List.Content style={{ display: 'flex', justifyContent: 'space-between' }}>
                    <span
                      style={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        maxWidth: 150,
                      }}
                    >
                      {session.title || t('chat.untitled')}
                    </span>
                    <span>
                      <Icon
                        name='pencil'
                        link
                        size='small'
                        onClick={(event) => {
                          event.stopPropagation();
                          setRenameTarget(session);
                          setRenameValue(session.title);
                        }}
                      />
                      <Icon
                        name='trash'
                        link
                        size='small'
                        color='red'
                        onClick={(event) => {
                          event.stopPropagation();
                          setDeleteTarget(session);
                        }}
                      />
                    </span>
                  </List.Content>
                </List.Item>
              ))}
            </List>
          </div>
          <div
            style={{
              padding: '8px 12px',
              borderTop: '1px solid #e8e8e8',
              color: '#666',
              fontSize: '0.85em',
              display: 'flex',
              justifyContent: 'space-between',
            }}
          >
            <span>{t('chat.balance')}</span>
            <span>{renderQuota(quota, t)}</span>
          </div>
          <div style={{ padding: 10, borderTop: '1px solid #e8e8e8', display: 'flex', gap: 6 }}>
            <Button basic fluid size='small' onClick={() => setToolsOpen(true)}>
              <Icon name='puzzle piece' /> {t('chat.toolsManager')}
            </Button>
            <Button
              basic
              size='small'
              icon='setting'
              disabled={!activeSession}
              onClick={() => setSettingsOpen(true)}
            />
          </div>
        </div>
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
          <div
            style={{
              padding: '8px 16px',
              borderBottom: '1px solid #e8e8e8',
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
            }}
          >
            <strong>{activeSession?.title || t('chat.title')}</strong>
            <span style={{ minWidth: 200 }}>
              <Dropdown
                search
                selection
                scrolling
                fluid
                size='small'
                placeholder={t('chat.model')}
                disabled={models.length === 0}
                options={models.map((model) => ({ key: model, text: model, value: model }))}
                value={activeSession ? activeSession.model : defaultModel || models[0] || ''}
                onChange={(event, data) => changeModel(String(data.value))}
              />
            </span>
          </div>
          <ChatMessageList messages={messages} emptyHint={t('chat.emptyHint')} />
          {streamError && (
            <div style={{ color: '#db2828', padding: '4px 16px', fontSize: '0.85em' }}>
              {streamError}
            </div>
          )}
          <ChatComposer
            value={input}
            onChange={setInput}
            onSend={send}
            onStop={stop}
            streaming={streaming}
            files={files}
            selectedFileIds={selectedFileIds}
            onToggleFile={toggleFile}
            onUpload={uploadFile}
            skills={skills}
            selectedSkillIds={selectedSkillIds}
            onToggleSkill={toggleSkill}
          />
        </div>
      </div>
      <ChatSettingsModal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        session={activeSession}
        models={models}
        skills={skills}
        bootstrap={bootstrap}
        onSave={saveSettings}
      />
      <ChatToolsModal open={toolsOpen} onClose={() => setToolsOpen(false)} />
      <Modal open={!!renameTarget} onClose={() => setRenameTarget(null)} size='mini'>
        <Modal.Header>{t('chat.rename')}</Modal.Header>
        <Modal.Content>
          <Input
            fluid
            value={renameValue}
            onChange={(event, data) => setRenameValue(String(data.value))}
          />
        </Modal.Content>
        <Modal.Actions>
          <Button onClick={() => setRenameTarget(null)}>{t('chat.cancel')}</Button>
          <Button color='blue' onClick={rename}>
            {t('chat.save')}
          </Button>
        </Modal.Actions>
      </Modal>
      <Confirm
        open={!!deleteTarget}
        content={t('chat.deleteConfirm')}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={removeSession}
      />
    </div>
  );
};

export default Chat;
