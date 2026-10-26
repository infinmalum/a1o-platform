import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Checkbox,
  Dropdown,
  Form,
  Input,
  Modal,
  TextArea,
} from 'semantic-ui-react';
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

  const [saving, setSaving] = React.useState(false);
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
    <Modal open={open} onClose={onClose} size='small' closeIcon>
      <Modal.Header>{t('chat.settings')}</Modal.Header>
      <Modal.Content>
        {value && (
          <Form>
            <Form.Field required>
              <label>{t('chat.model')}</label>
              <Dropdown
                selection
                search
                fluid
                options={models.map((model) => ({ key: model, text: model, value: model }))}
                value={value.model}
                onChange={(event, data) => update({ model: String(data.value) })}
              />
            </Form.Field>
            <Form.Field>
              <label>{t('chat.systemPrompt')}</label>
              <TextArea
                value={value.system_prompt}
                onChange={(event, data) => update({ system_prompt: String(data.value ?? '') })}
                style={{ minHeight: 90 }}
              />
            </Form.Field>
            <Form.Group widths='three'>
              <Form.Field>
                <label>temperature</label>
                <Input
                  type='number'
                  step='0.1'
                  min='0'
                  max='2'
                  value={value.temperature ?? ''}
                  onChange={(event, data) =>
                    update({ temperature: data.value === '' ? null : Number(data.value) })
                  }
                />
              </Form.Field>
              <Form.Field>
                <label>top_p</label>
                <Input
                  type='number'
                  step='0.1'
                  min='0'
                  max='1'
                  value={value.top_p ?? ''}
                  onChange={(event, data) =>
                    update({ top_p: data.value === '' ? null : Number(data.value) })
                  }
                />
              </Form.Field>
              <Form.Field>
                <label>max_tokens</label>
                <Input
                  type='number'
                  min='0'
                  max='16384'
                  value={value.max_tokens || ''}
                  onChange={(event, data) =>
                    update({ max_tokens: Math.round(Number(data.value) || 0) })
                  }
                />
              </Form.Field>
            </Form.Group>
            <Form.Field>
              <label>{t('chat.contextBudget')}</label>
              <Input
                type='number'
                min='0'
                placeholder={String(bootstrap?.budget ?? 32000)}
                value={value.context_budget || ''}
                onChange={(event, data) =>
                  update({ context_budget: Math.round(Number(data.value) || 0) })
                }
              />
            </Form.Field>
            <Form.Field>
              <label>{t('chat.sandbox')}</label>
              <Checkbox
                toggle
                checked={value.sandbox_enabled}
                disabled={!sandboxAllowed}
                onChange={() => update({ sandbox_enabled: !value.sandbox_enabled })}
                label={
                  sandboxAllowed
                    ? t('chat.sandboxHint')
                    : `${t('chat.sandboxHint')}（${t('chat.sandboxUnavailable')}）`
                }
              />
            </Form.Field>
            <Form.Field>
              <label>{t('chat.skills')}</label>
              <Dropdown
                multiple
                selection
                fluid
                options={skills
                  .filter((skill) => skill.enabled)
                  .map((skill) => ({ key: skill.id, text: skill.name, value: skill.id }))}
                value={value.skill_ids}
                onChange={(event, data) => update({ skill_ids: (data.value as number[]) || [] })}
              />
            </Form.Field>
          </Form>
        )}
      </Modal.Content>
      <Modal.Actions>
        <Button onClick={onClose}>{t('chat.cancel')}</Button>
        <Button color='blue' loading={saving} disabled={!value} onClick={save}>
          {t('chat.save')}
        </Button>
      </Modal.Actions>
    </Modal>
  );
};

export default ChatSettingsModal;
