import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Archive,
  ArchiveRestore,
  Copy,
  Loader2,
  MessageCircle,
  Pencil,
  Plus,
  Sparkles,
  Wrench,
} from 'lucide-react';

import { isAdmin } from '@/helpers/utils';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import {
  agentsApi,
  type AgentDraft,
  type AgentMode,
  type AgentPayload,
  type AgentView,
  type ChatFileLite,
} from './agentsApi';

interface AgentFormState {
  name: string;
  description: string;
  instructions: string;
  model: string;
  opening_hint: string;
  skill_ids: number[];
  file_ids: number[];
  sandbox_enabled: boolean;
  modes: AgentMode[];
}

const emptyForm: AgentFormState = {
  name: '',
  description: '',
  instructions: '',
  model: '',
  opening_hint: '',
  skill_ids: [],
  file_ids: [],
  sandbox_enabled: false,
  modes: ['chat', 'work'],
};

function formFromAgent(agent: AgentView): AgentFormState {
  return {
    name: agent.name,
    description: agent.description,
    instructions: agent.spec?.instructions || '',
    model: agent.spec?.model || '',
    opening_hint: agent.spec?.opening_hint || '',
    skill_ids: agent.spec?.skill_ids || [],
    file_ids: agent.spec?.file_ids || [],
    sandbox_enabled: !!agent.spec?.sandbox_enabled,
    modes: agent.spec?.modes?.length ? agent.spec.modes : ['chat', 'work'],
  };
}

// Agent plaza (R4): two sections — built-in agents maintained by admins and
// the caller's own. Saving an edit appends a new immutable version; running
// agents from a card pins that version on the session or task.
const Agents: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [builtin, setBuiltin] = useState<AgentView[]>([]);
  const [mine, setMine] = useState<AgentView[]>([]);
  const [skills, setSkills] = useState<{ id: number; name: string; enabled: boolean }[]>([]);
  const [files, setFiles] = useState<ChatFileLite[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AgentView | null>(null);
  const [form, setForm] = useState<AgentFormState>(emptyForm);
  const [builtinTarget, setBuiltinTarget] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [draftOpen, setDraftOpen] = useState(false);
  const [draftDescription, setDraftDescription] = useState('');
  const [draftModel, setDraftModel] = useState('');
  const [drafting, setDrafting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await agentsApi.listPlaza();
      setBuiltin(data.builtin || []);
      setMine(data.mine || []);
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    agentsApi.listSkills().then(setSkills).catch(() => undefined);
    agentsApi.listModels().then(setModels).catch(() => undefined);
    agentsApi.listFiles().then(setFiles).catch(() => undefined);
  }, [load]);

  const openCreate = (builtin: boolean) => {
    setEditing(null);
    setBuiltinTarget(builtin);
    setForm(emptyForm);
    setEditorOpen(true);
  };

  const openEdit = (agent: AgentView) => {
    setEditing(agent);
    setBuiltinTarget(agent.owner_id === 0);
    setForm(formFromAgent(agent));
    setEditorOpen(true);
  };

  const submit = async () => {
    if (!form.name.trim() || form.modes.length === 0 || submitting) return;
    setSubmitting(true);
    const payload: AgentPayload = {
      name: form.name.trim(),
      description: form.description.trim(),
      instructions: form.instructions,
      model: form.model,
      opening_hint: form.opening_hint,
      skill_ids: form.skill_ids,
      file_ids: form.file_ids,
      sandbox_enabled: form.sandbox_enabled,
      modes: form.modes,
    };
    try {
      if (editing) {
        await agentsApi.update(editing.id, payload);
      } else if (builtinTarget) {
        await agentsApi.createBuiltin(payload);
      } else {
        await agentsApi.create(payload);
      }
      setEditorOpen(false);
      await load();
    } catch {
      // toast already surfaced
    } finally {
      setSubmitting(false);
    }
  };

  const toggleArchived = async (agent: AgentView) => {
    try {
      await agentsApi.setArchived(agent.id, !agent.archived);
      await load();
    } catch {
      // toast already surfaced
    }
  };

  const duplicate = async (agent: AgentView) => {
    try {
      await agentsApi.duplicate(agent.id);
      await load();
    } catch {
      // toast already surfaced by the client helper
    }
  };

  const generateDraft = async () => {
    if (!draftDescription.trim() || drafting) return;
    setDrafting(true);
    try {
      const draft: AgentDraft = await agentsApi.draft(draftDescription.trim(), draftModel || form.model);
      setForm((current) => ({
        ...current,
        name: draft.name || current.name,
        description: draft.description || current.description,
        instructions: draft.instructions || current.instructions,
        opening_hint: draft.opening_hint || current.opening_hint,
      }));
      setDraftOpen(false);
      setDraftDescription('');
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setDrafting(false);
    }
  };

  const startChat = (agent: AgentView) => navigate(`/chat?agent=${agent.id}`);
  const startWork = (agent: AgentView) => navigate(`/work?agent=${agent.id}`);

  const modeBadge = (modes: AgentMode[]) =>
    modes.map((mode) => (
      <Badge key={mode} variant='outline'>
        {t(`agents.mode.${mode}`)}
      </Badge>
    ));

  const renderCard = (agent: AgentView, own: boolean) => (
    <Card key={agent.id} className={agent.archived ? 'opacity-70' : undefined}>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-base'>
          {agent.name}
          {own && <Badge variant='secondary'>v{agent.current_version}</Badge>}
          {agent.archived && <Badge variant='destructive'>{t('agents.archived')}</Badge>}
        </CardTitle>
        <CardDescription className='line-clamp-2'>
          {agent.description || t('agents.noDescription')}
        </CardDescription>
        <CardAction>{modeBadge(agent.spec?.modes || [])}</CardAction>
      </CardHeader>
      {(own || isAdmin()) && (
        <CardContent>
          <p className='text-muted-foreground line-clamp-2 text-xs'>
            {agent.spec?.instructions || t('agents.noInstructions')}
          </p>
        </CardContent>
      )}
      <CardFooter className='flex flex-wrap gap-2'>
        {agent.spec?.modes?.includes('chat') && !agent.archived && (
          <Button size='sm' variant='outline' onClick={() => startChat(agent)}>
            <MessageCircle /> {t('agents.startChat')}
          </Button>
        )}
        {!agent.archived && (
          <Button size='sm' variant='ghost' onClick={() => duplicate(agent)}>
            <Copy /> {t('agents.duplicate')}
          </Button>
        )}
        {agent.spec?.modes?.includes('work') && !agent.archived && (
          <Button size='sm' variant='outline' onClick={() => startWork(agent)}>
            <Wrench /> {t('agents.startWork')}
          </Button>
        )}
        {(own || (isAdmin() && agent.owner_id === 0)) && !agent.archived && (
          <Button size='sm' variant='ghost' onClick={() => openEdit(agent)}>
            <Pencil /> {t('agents.edit')}
          </Button>
        )}
        {(own || (isAdmin() && agent.owner_id === 0)) && (
          <Button size='sm' variant='ghost' onClick={() => toggleArchived(agent)}>
            {agent.archived ? <ArchiveRestore /> : <Archive />}
            {agent.archived ? t('agents.unarchive') : t('agents.archive')}
          </Button>
        )}
      </CardFooter>
    </Card>
  );

  const toggleMode = (mode: AgentMode, checked: boolean) => {
    setForm((current) => {
      const modes = checked
        ? [...current.modes, mode]
        : current.modes.filter((item) => item !== mode);
      return { ...current, modes };
    });
  };

  const toggleSkill = (id: number, checked: boolean) => {
    setForm((current) => ({
      ...current,
      skill_ids: checked
        ? [...current.skill_ids, id]
        : current.skill_ids.filter((item) => item !== id),
    }));
  };

  const toggleFile = (id: number, checked: boolean) => {
    setForm((current) => ({
      ...current,
      file_ids: checked
        ? [...current.file_ids, id]
        : current.file_ids.filter((item) => item !== id),
    }));
  };

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-10'>
      <div className='mb-8 flex items-center justify-between'>
        <div>
          <h1 className='text-2xl font-semibold tracking-tight'>{t('agents.title')}</h1>
          <p className='text-muted-foreground mt-1 text-sm'>{t('agents.tagline')}</p>
        </div>
        <div className='flex items-center gap-2'>
          {isAdmin() && (
            <Button variant='outline' size='sm' onClick={() => openCreate(true)}>
              <Plus /> {t('agents.createBuiltin')}
            </Button>
          )}
          <Button size='sm' onClick={() => openCreate(false)}>
            <Plus /> {t('agents.create')}
          </Button>
        </div>
      </div>

      {loading ? (
        <p className='text-muted-foreground text-sm'>{t('agents.loading')}</p>
      ) : (
        <>
          <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('agents.builtinSection')}</h2>
          {builtin.length === 0 ? (
            <p className='text-muted-foreground mb-6 text-sm'>{t('agents.emptyBuiltin')}</p>
          ) : (
            <div className='mb-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              {builtin.map((agent) => renderCard(agent, false))}
            </div>
          )}
          <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('agents.mineSection')}</h2>
          {mine.length === 0 ? (
            <p className='text-muted-foreground text-sm'>{t('agents.emptyMine')}</p>
          ) : (
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              {mine.map((agent) => renderCard(agent, true))}
            </div>
          )}
        </>
      )}

      <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
        <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle>
              {editing ? t('agents.editTitle') : builtinTarget ? t('agents.createBuiltin') : t('agents.createTitle')}
            </DialogTitle>
            <DialogDescription>{t('agents.editHint')}</DialogDescription>
          </DialogHeader>
          <div className='grid gap-4 py-2'>
            <div className='grid gap-2'>
              <Label htmlFor='agent-name'>{t('agents.nameLabel')}</Label>
              <Input
                id='agent-name'
                value={form.name}
                onChange={(event) => setForm({ ...form, name: event.target.value })}
                placeholder={t('agents.namePlaceholder')}
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='agent-desc'>{t('agents.descLabel')}</Label>
              <Input
                id='agent-desc'
                value={form.description}
                onChange={(event) => setForm({ ...form, description: event.target.value })}
                placeholder={t('agents.descPlaceholder')}
              />
            </div>
            {!editing && (
              <div className='flex items-center justify-between rounded-md border bg-muted/30 p-2'>
                <span className='text-muted-foreground text-xs'>{t('agents.draftHint')}</span>
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => {
                    // Preselect only the form's own model choice: capability
                    // cannot be guessed from names, and an unpriced or
                    // non-chat pick surfaces a precise gateway error.
                    setDraftModel((current) => current || form.model);
                    setDraftOpen(true);
                  }}
                >
                  <Sparkles /> {t('agents.draftButton')}
                </Button>
              </div>
            )}
            <div className='grid gap-2'>
              <Label htmlFor='agent-instructions'>{t('agents.instructionsLabel')}</Label>
              <Textarea
                id='agent-instructions'
                value={form.instructions}
                onChange={(event) => setForm({ ...form, instructions: event.target.value })}
                placeholder={t('agents.instructionsPlaceholder')}
                rows={5}
              />
            </div>
            <div className='grid gap-2'>
              <Label>{t('agents.modelLabel')}</Label>
              <Select value={form.model} onValueChange={(model) => setForm({ ...form, model })}>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('agents.modelPlaceholder')} />
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
            <div className='grid gap-2'>
              <Label htmlFor='agent-opening'>{t('agents.openingLabel')}</Label>
              <Input
                id='agent-opening'
                value={form.opening_hint}
                onChange={(event) => setForm({ ...form, opening_hint: event.target.value })}
                placeholder={t('agents.openingPlaceholder')}
              />
            </div>
            <div className='grid gap-2'>
              <Label>{t('agents.skillsLabel')}</Label>
              {skills.length === 0 ? (
                <p className='text-muted-foreground text-xs'>{t('agents.noSkills')}</p>
              ) : (
                <div className='grid max-h-32 gap-1.5 overflow-y-auto rounded-md border p-2'>
                  {skills.map((skill) => (
                    <label
                      key={skill.id}
                      className='flex cursor-pointer items-center gap-2 text-sm'
                    >
                      <Checkbox
                        checked={form.skill_ids.includes(skill.id)}
                        onCheckedChange={(checked) => toggleSkill(skill.id, checked === true)}
                      />
                      {skill.name}
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className='grid gap-2'>
              <Label>{t('agents.filesLabel')}</Label>
              {files.length === 0 ? (
                <p className='text-muted-foreground text-xs'>{t('agents.noFiles')}</p>
              ) : (
                <div className='grid max-h-32 gap-1.5 overflow-y-auto rounded-md border p-2'>
                  {files.map((file) => (
                    <label
                      key={file.id}
                      className='flex cursor-pointer items-center gap-2 text-sm'
                    >
                      <Checkbox
                        checked={form.file_ids.includes(file.id)}
                        onCheckedChange={(checked) => toggleFile(file.id, checked === true)}
                      />
                      {file.name}
                      <span className='text-muted-foreground ml-auto text-xs'>
                        {file.kind === 'image' ? t('agents.fileImage') : t('agents.fileText')}
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className='flex items-center justify-between'>
              <Label htmlFor='agent-sandbox'>{t('agents.sandboxLabel')}</Label>
              <Switch
                id='agent-sandbox'
                checked={form.sandbox_enabled}
                onCheckedChange={(checked) => setForm({ ...form, sandbox_enabled: checked })}
              />
            </div>
            <div className='grid gap-2'>
              <Label>{t('agents.modesLabel')}</Label>
              <div className='flex gap-4'>
                {(['chat', 'work'] as AgentMode[]).map((mode) => (
                  <label key={mode} className='flex cursor-pointer items-center gap-2 text-sm'>
                    <Checkbox
                      checked={form.modes.includes(mode)}
                      onCheckedChange={(checked) => toggleMode(mode, checked === true)}
                    />
                    {t(`agents.mode.${mode}`)}
                  </label>
                ))}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setEditorOpen(false)}>
              {t('agents.cancel')}
            </Button>
            <Button
              onClick={submit}
              disabled={!form.name.trim() || form.modes.length === 0 || submitting}
            >
              {editing ? t('agents.saveVersion') : t('agents.submit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={draftOpen} onOpenChange={setDraftOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('agents.draftTitle')}</DialogTitle>
            <DialogDescription>{t('agents.draftDialogHint')}</DialogDescription>
          </DialogHeader>
          <div className='grid gap-4 py-2'>
            <div className='grid gap-2'>
              <Label htmlFor='agent-draft'>{t('agents.draftInputLabel')}</Label>
              <Textarea
                id='agent-draft'
                value={draftDescription}
                onChange={(event) => setDraftDescription(event.target.value)}
                placeholder={t('agents.draftInputPlaceholder')}
                rows={4}
              />
            </div>
            <div className='grid gap-2'>
              <Label>{t('agents.draftModelLabel')}</Label>
              <Select value={draftModel} onValueChange={setDraftModel}>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('agents.modelPlaceholder')} />
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
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDraftOpen(false)}>
              {t('agents.cancel')}
            </Button>
            <Button onClick={generateDraft} disabled={!draftDescription.trim() || drafting}>
              {drafting ? <Loader2 className='animate-spin' /> : <Sparkles />}
              {t('agents.draftGenerate')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default Agents;
