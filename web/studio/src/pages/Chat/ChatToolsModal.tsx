import React from 'react';
import { useTranslation } from 'react-i18next';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { chatApi, type ChatMCPServer, type ChatSkill } from './chatApi';

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
      <Button size='sm' variant='outline' className='mb-3' onClick={() => startEdit({ ...emptySkill })}>
        {t('chat.newSkill')}
      </Button>
      {skills.map((skill) => (
        <div key={skill.id} className='mb-2 rounded-md border p-3'>
          <div className='flex items-start justify-between gap-2'>
            <div className='text-sm font-medium'>
              {skill.name}{' '}
              <span className='text-muted-foreground text-xs font-normal'>
                {skill.owner_id === 0 ? t('chat.globalSkill') : t('chat.personalSkill')}
                {!skill.enabled && ` · ${t('chat.disabled')}`}
              </span>
            </div>
            {skill.owner_id !== 0 && (
              <div className='flex gap-1'>
                <Button size='sm' variant='ghost' onClick={() => startEdit(skill)}>
                  {t('chat.edit')}
                </Button>
                <Button size='sm' variant='ghost' className='text-destructive' onClick={() => remove(skill.id)}>
                  {t('chat.delete')}
                </Button>
              </div>
            )}
          </div>
          {skill.description && <div className='text-muted-foreground mt-1 text-sm'>{skill.description}</div>}
          {skill.instructions && (
            <div className='mt-1 max-h-20 overflow-hidden text-xs whitespace-pre-wrap text-foreground/70'>
              {skill.instructions}
            </div>
          )}
          {skill.tools && skill.tools.length > 0 && (
            <div className='mt-2 flex flex-wrap gap-1'>
              {skill.tools.map((tool) => (
                <Badge key={tool} variant='secondary'>
                  {tool}
                </Badge>
              ))}
            </div>
          )}
        </div>
      ))}
      <Dialog open={!!editing} onOpenChange={(next) => (!next ? setEditing(null) : undefined)}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('chat.editSkill')}</DialogTitle>
          </DialogHeader>
          {editing && (
            <div className='grid gap-4'>
              <div className='grid gap-2'>
                <Label>{t('chat.skillName')}</Label>
                <Input
                  value={editing.name}
                  onChange={(event) => setEditing({ ...editing, name: event.target.value })}
                />
              </div>
              <div className='grid gap-2'>
                <Label>{t('chat.skillDescription')}</Label>
                <Input
                  value={editing.description}
                  onChange={(event) => setEditing({ ...editing, description: event.target.value })}
                />
              </div>
              <div className='grid gap-2'>
                <Label>{t('chat.skillInstructions')}</Label>
                <Textarea
                  value={editing.instructions}
                  onChange={(event) => setEditing({ ...editing, instructions: event.target.value })}
                  className='min-h-28'
                />
              </div>
              <div className='grid gap-2'>
                <Label>{t('chat.skillTools')}</Label>
                <Input
                  value={toolText}
                  placeholder='sandbox, mcp_server_tool'
                  onChange={(event) => setToolText(event.target.value)}
                />
              </div>
              <div className='flex items-center justify-between rounded-md border p-3'>
                <span className='text-sm'>{t('chat.enabled')}</span>
                <Switch
                  checked={editing.enabled}
                  onCheckedChange={(checked) => setEditing({ ...editing, enabled: checked })}
                />
              </div>
            </div>
          )}
          <div className='flex justify-end gap-2'>
            <Button variant='outline' onClick={() => setEditing(null)}>
              {t('chat.cancel')}
            </Button>
            <Button onClick={save}>{t('chat.save')}</Button>
          </div>
        </DialogContent>
      </Dialog>
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
      <Button size='sm' variant='outline' className='mb-3' onClick={() => startEdit({ ...emptyServer })}>
        {t('chat.newMCPServer')}
      </Button>
      {servers.map((server) => (
        <div key={server.id} className='mb-2 rounded-md border p-3'>
          <div className='flex items-start justify-between gap-2'>
            <div className='text-sm font-medium'>
              {server.name}{' '}
              <span className='text-muted-foreground text-xs font-normal'>
                {server.url} · {!server.enabled && t('chat.disabled')}
              </span>
            </div>
            <div className='flex gap-1'>
              <Button size='sm' variant='ghost' onClick={() => startEdit(server)}>
                {t('chat.edit')}
              </Button>
              <Button size='sm' variant='ghost' className='text-destructive' onClick={() => remove(server.id)}>
                {t('chat.delete')}
              </Button>
            </div>
          </div>
        </div>
      ))}
      <Dialog open={!!editing} onOpenChange={(next) => (!next ? setEditing(null) : undefined)}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('chat.editMCPServer')}</DialogTitle>
            <DialogDescription>HTTP</DialogDescription>
          </DialogHeader>
          {editing && (
            <div className='grid gap-4'>
              <div className='grid gap-2'>
                <Label>{t('chat.mcpName')}</Label>
                <Input
                  value={editing.name}
                  onChange={(event) => setEditing({ ...editing, name: event.target.value })}
                />
              </div>
              <div className='grid gap-2'>
                <Label>URL</Label>
                <Input
                  placeholder='https://example.com/mcp'
                  value={editing.url}
                  onChange={(event) => setEditing({ ...editing, url: event.target.value })}
                />
              </div>
              <div className='grid gap-2'>
                <Label>{t('chat.mcpHeaders')}</Label>
                <Textarea
                  value={headersText}
                  placeholder={'Authorization: Bearer sk-xxx'}
                  onChange={(event) => setHeadersText(event.target.value)}
                  className='min-h-20'
                />
              </div>
              <div className='flex items-center justify-between rounded-md border p-3'>
                <span className='text-sm'>{t('chat.enabled')}</span>
                <Switch
                  checked={editing.enabled}
                  onCheckedChange={(checked) => setEditing({ ...editing, enabled: checked })}
                />
              </div>
            </div>
          )}
          <div className='flex justify-end gap-2'>
            <Button variant='outline' onClick={() => setEditing(null)}>
              {t('chat.cancel')}
            </Button>
            <Button onClick={save}>{t('chat.save')}</Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
};

const ChatToolsModal: React.FC<{ open: boolean; onClose: () => void }> = ({ open, onClose }) => {
  const { t } = useTranslation();
  const [tab, setTab] = React.useState<'skills' | 'mcp'>('skills');

  return (
    <Dialog open={open} onOpenChange={(next) => (!next ? onClose() : undefined)}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('chat.toolsManager')}</DialogTitle>
        </DialogHeader>
        <div className='flex gap-1 rounded-md bg-muted p-1'>
          {(
            [
              ['skills', t('chat.skills')],
              ['mcp', t('chat.myMCPServers')],
            ] as const
          ).map(([key, label]) => (
            <button
              key={key}
              type='button'
              onClick={() => setTab(key)}
              className={
                tab === key
                  ? 'bg-background shadow-xs rounded-sm px-3 py-1.5 text-sm font-medium'
                  : 'text-muted-foreground hover:text-foreground rounded-sm px-3 py-1.5 text-sm'
              }
            >
              {label}
            </button>
          ))}
        </div>
        <div className='max-h-[60vh] overflow-y-auto pr-1'>
          {tab === 'skills' ? <SkillPane /> : <UserMCPPane />}
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default ChatToolsModal;
