import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Bot, MessageSquarePlus, Pencil, Save, Settings2, Trash2, Wrench } from 'lucide-react';

import { API } from '@/helpers/api';
import { renderQuota } from '@/helpers/render';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import ChatMessageList, { type DisplayMessage } from './ChatMessageList';
import ChatComposer from './ChatComposer';
import ChatSettingsModal, { type ChatSettingsValue } from './ChatSettingsModal';
import ChatToolsModal from './ChatToolsModal';
import { agentsApi, type AgentView } from '@/pages/Agents/agentsApi';
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
  const [searchParams, setSearchParams] = useSearchParams();
  const [sessions, setSessions] = React.useState<ChatSession[]>([]);
  const [activeSession, setActiveSession] = React.useState<ChatSession | null>(null);
  const [agents, setAgents] = React.useState<AgentView[]>([]);
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
  const [saveAgentOpen, setSaveAgentOpen] = React.useState(false);
  const [saveAgentName, setSaveAgentName] = React.useState('');
  const [saveAgentDesc, setSaveAgentDesc] = React.useState('');
  const [savingAgent, setSavingAgent] = React.useState(false);
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
          ? { id: row.tool_call_id || '', name: row.name || '', content: row.content, isError: false }
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
    agentsApi
      .listPlaza()
      .then((data) => setAgents([...(data.builtin || []), ...(data.mine || [])]))
      .catch(() => undefined);
  }, [reloadSessions]);

  // Deep link from the agent plaza: /chat?agent=<id> creates a session
  // pinned to the agent's latest immutable version, then clears the query.
  const agentParam = searchParams.get('agent');
  React.useEffect(() => {
    if (!agentParam) return;
    let cancelled = false;
    agentsApi
      .getDetail(Number(agentParam))
      .then(async (agent) => {
        if (cancelled) return;
        const fallback = defaultModel || models[0] || 'gpt-4o-mini';
        const session = await chatApi.createSession({
          model: agent.spec?.model || fallback,
          agent_version_id: agent.latest.id,
        });
        if (cancelled) return;
        setSearchParams({}, { replace: true });
        await reloadSessions();
        setActiveSession(session);
        setMessages([]);
        setSelectedSkillIds(session.skill_ids || []);
        setSelectedFileIds([]);
      })
      .catch(() => {
        if (!cancelled) setSearchParams({}, { replace: true });
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentParam]);

  // Deep link from the workspace history: /chat?session=<id> opens it.
  const sessionParam = searchParams.get('session');
  React.useEffect(() => {
    if (!sessionParam || sessions.length === 0) return;
    const target = sessions.find((item) => item.id === Number(sessionParam));
    if (target) {
      setSearchParams({}, { replace: true });
      openSession(target);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionParam, sessions]);

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
    const payload = event.payload || {};
    setMessages((current) => {
      const next = [...current];
      const last = next[next.length - 1];
      switch (event.type) {
        case 'message.started':
          next.push({
            id: payload.message_id || -Date.now(),
            role: 'assistant',
            content: '',
            streaming: true,
          });
          break;
        case 'response.output_text.delta':
          if (last && last.role === 'assistant' && last.streaming) {
            next[next.length - 1] = { ...last, content: last.content + (payload.delta || '') };
          }
          break;
        case 'tool.called':
          if (last && last.role === 'assistant') {
            next[next.length - 1] = {
              ...last,
              streaming: false,
              toolCalls: [
                ...(last.toolCalls || []),
                {
                  id: payload.tool_call_id || '',
                  name: payload.name || '',
                  arguments: payload.arguments || '',
                },
              ],
            };
          }
          break;
        case 'tool.result':
          next.push({
            id: -(Date.now() + next.length),
            role: 'tool',
            content: payload.content || '',
            toolResult: {
              id: payload.tool_call_id || '',
              name: payload.name || '',
              content: payload.content || '',
              isError: !!payload.is_error,
            },
          });
          break;
        case 'context.compacted':
          next.push({
            id: -(Date.now() + next.length),
            role: 'assistant',
            content: '',
            compacted: true,
          });
          break;
        case 'run.failed':
          setStreamError(payload.error || t('chat.requestFailed'));
          if (last && last.role === 'assistant') {
            next[next.length - 1] = { ...last, streaming: false };
          }
          break;
        case 'run.completed':
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

  // The pinned agent's frozen version id resolves through the plaza list;
  // sessions bound to an older version simply show no badge.
  const activeAgent = activeSession?.agent_version_id
    ? agents.find((agent) => agent.latest?.id === activeSession.agent_version_id)
    : undefined;
  const emptyHint =
    activeAgent?.spec?.opening_hint || t('chat.emptyHint');

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

  // Save the active session's settings as a reusable personal agent draft
  // (P2a): model, system prompt, sampling, skills and sandbox carry over;
  // the agent starts as chat-only and version 1.
  const saveSessionAsAgent = async () => {
    if (!activeSession || savingAgent) return;
    setSavingAgent(true);
    try {
      await agentsApi.create({
        name: saveAgentName.trim() || activeSession.title || t('chat.saveAgentDefaultName'),
        description: saveAgentDesc.trim(),
        instructions: activeSession.system_prompt || '',
        model: activeSession.model,
        skill_ids: selectedSkillIds,
        file_ids: [],
        sandbox_enabled: activeSession.sandbox_enabled,
        modes: ['chat'],
        opening_hint: '',
      });
      setSaveAgentOpen(false);
      setSaveAgentName('');
      setSaveAgentDesc('');
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setSavingAgent(false);
    }
  };

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-4'>
      <div className='flex h-[calc(100vh-7rem)] min-h-[480px] overflow-hidden rounded-xl border'>
        <div className='flex w-64 flex-col border-r bg-muted/40'>
          <div className='p-2.5'>
            <Button className='w-full' onClick={createSession}>
              <MessageSquarePlus /> {t('chat.newChat')}
            </Button>
          </div>
          <div className='flex-1 overflow-y-auto px-2'>
            {sessions.map((session) => (
              <div
                key={session.id}
                onClick={() => openSession(session)}
                className={
                  'group mb-0.5 flex cursor-pointer items-center justify-between rounded-md px-2.5 py-2 text-sm ' +
                  (activeSession?.id === session.id ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60')
                }
              >
                <span className='max-w-[150px] truncate'>
                  {session.title || t('chat.untitled')}
                </span>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild onClick={(event) => event.stopPropagation()}>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      className='opacity-0 transition-opacity group-hover:opacity-100'
                      onClick={(event) => event.stopPropagation()}
                    >
                      <Pencil className='size-3.5' />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align='end'>
                    <DropdownMenuItem
                      onClick={(event) => {
                        event.stopPropagation();
                        setRenameTarget(session);
                        setRenameValue(session.title);
                      }}
                    >
                      <Pencil /> {t('chat.rename')}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      variant='destructive'
                      onClick={(event) => {
                        event.stopPropagation();
                        setDeleteTarget(session);
                      }}
                    >
                      <Trash2 /> {t('chat.delete')}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            ))}
          </div>
          <div className='text-muted-foreground flex items-center justify-between border-t px-3 py-2 text-xs'>
            <span>{t('chat.balance')}</span>
            <span>{renderQuota(quota, t)}</span>
          </div>
          <div className='flex gap-1.5 border-t p-2.5'>
            <Button variant='outline' size='sm' className='flex-1' onClick={() => setToolsOpen(true)}>
              <Wrench /> {t('chat.toolsManager')}
            </Button>
            <Button
              variant='outline'
              size='icon-sm'
              disabled={!activeSession}
              onClick={() => {
                setSaveAgentName(activeSession?.title || '');
                setSaveAgentDesc('');
                setSaveAgentOpen(true);
              }}
              title={t('chat.saveAgent')}
            >
              <Save />
            </Button>
            <Button
              variant='outline'
              size='icon-sm'
              disabled={!activeSession}
              onClick={() => setSettingsOpen(true)}
              title={t('chat.settings')}
            >
              <Settings2 />
            </Button>
          </div>
        </div>
        <div className='flex flex-1 flex-col'>
          <div className='flex items-center justify-between border-b px-4 py-2'>
            <div className='flex items-center gap-2'>
              <strong className='text-sm'>{activeSession?.title || t('chat.title')}</strong>
              {activeAgent && (
                <Badge variant='secondary' className='gap-1'>
                  <Bot className='size-3' />
                  {activeAgent.name}
                </Badge>
              )}
            </div>
            <Select
              value={activeSession ? activeSession.model : defaultModel || models[0] || ''}
              onValueChange={changeModel}
              disabled={models.length === 0}
            >
              <SelectTrigger size='sm' className='w-52'>
                <SelectValue placeholder={t('chat.model')} />
              </SelectTrigger>
              <SelectContent className='max-h-72'>
                {models.map((model) => (
                  <SelectItem key={model} value={model}>
                    {model}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <ChatMessageList messages={messages} emptyHint={emptyHint} />
          {streamError && (
            <div className='text-destructive px-4 py-1 text-xs'>{streamError}</div>
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
      <Dialog open={!!renameTarget} onOpenChange={(next) => (!next ? setRenameTarget(null) : undefined)}>
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>{t('chat.rename')}</DialogTitle>
          </DialogHeader>
          <Input value={renameValue} onChange={(event) => setRenameValue(event.target.value)} />
          <DialogFooter>
            <Button variant='outline' onClick={() => setRenameTarget(null)}>
              {t('chat.cancel')}
            </Button>
            <Button onClick={rename}>{t('chat.save')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={!!deleteTarget} onOpenChange={(next) => (!next ? setDeleteTarget(null) : undefined)}>
        <DialogContent className='sm:max-w-sm'>
          <DialogHeader>
            <DialogTitle>{t('chat.deleteConfirm')}</DialogTitle>
          </DialogHeader>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDeleteTarget(null)}>
              {t('chat.cancel')}
            </Button>
            <Button variant='destructive' onClick={removeSession}>
              {t('chat.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={saveAgentOpen} onOpenChange={setSaveAgentOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('chat.saveAgentTitle')}</DialogTitle>
            <DialogDescription>{t('chat.saveAgentHint')}</DialogDescription>
          </DialogHeader>
          <div className='grid gap-4 py-2'>
            <div className='grid gap-2'>
              <Label htmlFor='save-agent-name'>{t('agents.nameLabel')}</Label>
              <Input
                id='save-agent-name'
                value={saveAgentName}
                onChange={(event) => setSaveAgentName(event.target.value)}
                placeholder={t('agents.namePlaceholder')}
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='save-agent-desc'>{t('agents.descLabel')}</Label>
              <Input
                id='save-agent-desc'
                value={saveAgentDesc}
                onChange={(event) => setSaveAgentDesc(event.target.value)}
                placeholder={t('agents.descPlaceholder')}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setSaveAgentOpen(false)}>
              {t('chat.cancel')}
            </Button>
            <Button onClick={saveSessionAsAgent} disabled={savingAgent}>
              <Bot /> {t('chat.saveAgentSubmit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default Chat;
