import React from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
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
import type { ChatBootstrap, ChatSession, ChatSkill } from './chatApi';

export interface ChatSettingsValue {
  model: string;
  system_prompt: string;
  temperature: number | null;
  top_p: number | null;
  max_tokens: number;
  sandbox_enabled: boolean;
  context_budget: number;
  skill_ids: number[];
}

const ChatSettingsModal: React.FC<{
  open: boolean;
  onClose: () => void;
  session: ChatSession | null;
  models: string[];
  skills: ChatSkill[];
  bootstrap: ChatBootstrap | null;
  onSave: (value: ChatSettingsValue) => Promise<void>;
}> = ({ open, onClose, session, models, skills, bootstrap, onSave }) => {
  const { t } = useTranslation();
  const [value, setValue] = React.useState<ChatSettingsValue | null>(null);
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (open && session) {
      setValue({
        model: session.model,
        system_prompt: session.system_prompt,
        temperature: session.temperature,
        top_p: session.top_p,
        max_tokens: session.max_tokens,
        sandbox_enabled: session.sandbox_enabled,
        context_budget: session.context_budget,
        skill_ids: session.skill_ids || [],
      });
    }
  }, [open, session]);

  const update = (patch: Partial<ChatSettingsValue>) => {
    setValue((current) => (current ? { ...current, ...patch } : current));
  };

  const save = async () => {
    if (!value) return;
    setSaving(true);
    try {
      await onSave(value);
      onClose();
    } catch {
      // chatApi toasts the error; keep the modal open with the user's values
    } finally {
      setSaving(false);
    }
  };

  const sandboxAllowed = !!bootstrap && bootstrap.sandbox_available && bootstrap.sandbox_enabled;

  return (
    <Dialog open={open} onOpenChange={(next) => (!next ? onClose() : undefined)}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('chat.settings')}</DialogTitle>
          <DialogDescription>{session?.title}</DialogDescription>
        </DialogHeader>
        {value && (
          <div className='grid gap-4'>
            <div className='grid gap-2'>
              <Label>{t('chat.model')}</Label>
              <Select value={value.model} onValueChange={(model) => update({ model })}>
                <SelectTrigger className='w-full'>
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
            <div className='grid gap-2'>
              <Label>{t('chat.systemPrompt')}</Label>
              <Textarea
                value={value.system_prompt}
                onChange={(event) => update({ system_prompt: event.target.value })}
                className='min-h-24'
              />
            </div>
            <div className='grid grid-cols-3 gap-3'>
              <div className='grid gap-2'>
                <Label htmlFor='temperature'>temperature</Label>
                <Input
                  id='temperature'
                  type='number'
                  step='0.1'
                  min='0'
                  max='2'
                  value={value.temperature ?? ''}
                  onChange={(event) =>
                    update({ temperature: event.target.value === '' ? null : Number(event.target.value) })
                  }
                />
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='top_p'>top_p</Label>
                <Input
                  id='top_p'
                  type='number'
                  step='0.1'
                  min='0'
                  max='1'
                  value={value.top_p ?? ''}
                  onChange={(event) =>
                    update({ top_p: event.target.value === '' ? null : Number(event.target.value) })
                  }
                />
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='max_tokens'>max_tokens</Label>
                <Input
                  id='max_tokens'
                  type='number'
                  min='0'
                  max='16384'
                  value={value.max_tokens || ''}
                  onChange={(event) =>
                    update({ max_tokens: Math.round(Number(event.target.value) || 0) })
                  }
                />
              </div>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='context_budget'>{t('chat.contextBudget')}</Label>
              <Input
                id='context_budget'
                type='number'
                min='0'
                placeholder={String(bootstrap?.budget ?? 32000)}
                value={value.context_budget || ''}
                onChange={(event) =>
                  update({ context_budget: Math.round(Number(event.target.value) || 0) })
                }
              />
            </div>
            <div className='flex items-center justify-between gap-4 rounded-md border p-3'>
              <div className='text-sm'>
                <p>{t('chat.sandbox')}</p>
                <p className='text-muted-foreground text-xs'>
                  {t('chat.sandboxHint')}
                  {!sandboxAllowed && `（${t('chat.sandboxUnavailable')}）`}
                </p>
              </div>
              <Switch
                checked={value.sandbox_enabled}
                disabled={!sandboxAllowed}
                onCheckedChange={(checked) => update({ sandbox_enabled: checked })}
              />
            </div>
            <div className='grid gap-2'>
              <Label>{t('chat.skills')}</Label>
              <div className='max-h-40 space-y-1.5 overflow-y-auto rounded-md border p-3'>
                {skills.filter((skill) => skill.enabled).length === 0 && (
                  <p className='text-muted-foreground text-sm'>{t('chat.noSkills')}</p>
                )}
                {skills
                  .filter((skill) => skill.enabled)
                  .map((skill) => (
                    <label
                      key={skill.id}
                      className='flex cursor-pointer items-center gap-2 text-sm'
                    >
                      <Checkbox
                        checked={value.skill_ids.includes(skill.id)}
                        onCheckedChange={() =>
                          update({
                            skill_ids: value.skill_ids.includes(skill.id)
                              ? value.skill_ids.filter((id) => id !== skill.id)
                              : [...value.skill_ids, skill.id],
                          })
                        }
                      />
                      {skill.name}
                    </label>
                  ))}
              </div>
            </div>
          </div>
        )}
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            {t('chat.cancel')}
          </Button>
          <Button onClick={save} disabled={!value || saving}>
            {t('chat.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default ChatSettingsModal;
