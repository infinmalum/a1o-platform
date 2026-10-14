import React from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Dropdown, Icon, Label, TextArea } from 'semantic-ui-react';
import type { ChatFile, ChatSkill } from './chatApi';

interface ChatComposerProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onStop: () => void;
  streaming: boolean;
  files: ChatFile[];
  selectedFileIds: number[];
  onToggleFile: (id: number) => void;
  onUpload: (file: File) => void;
  skills: ChatSkill[];
  selectedSkillIds: number[];
  onToggleSkill: (id: number) => void;
}

const ChatComposer: React.FC<ChatComposerProps> = ({
  value,
  onChange,
  onSend,
  onStop,
  streaming,
  files,
  selectedFileIds,
  onToggleFile,
  onUpload,
  skills,
  selectedSkillIds,
  onToggleSkill,
}) => {
  const { t } = useTranslation();
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      if (!streaming) {
        onSend();
      }
    }
  };

  const skillOptions = skills
    .filter((skill) => skill.enabled)
    .map((skill) => ({
      key: skill.id,
      text: skill.name,
      value: skill.id,
      description: skill.description,
    }));

  return (
    <div style={{ borderTop: '1px solid #e8e8e8', padding: '10px 16px 14px' }}>
      {(selectedFileIds.length > 0 || selectedSkillIds.length > 0) && (
        <div style={{ marginBottom: 8 }}>
          {selectedFileIds.map((id) => {
            const file = files.find((item) => item.id === id);
            return (
              <Label key={`f-${id}`} size='tiny' color='blue' onClick={() => onToggleFile(id)}>
                <Icon name='delete' />
                {file ? file.name : `#${id}`}
              </Label>
            );
          })}
          {selectedSkillIds.map((id) => {
            const skill = skills.find((item) => item.id === id);
            return (
              <Label key={`s-${id}`} size='tiny' color='violet' onClick={() => onToggleSkill(id)}>
                <Icon name='delete' />
                {skill ? skill.name : `#${id}`}
              </Label>
            );
          })}
        </div>
      )}
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 8 }}>
        <div>
          <Button
            icon
            basic
            size='small'
            title={t('chat.attachFile') || ''}
            onClick={() => fileInputRef.current?.click()}
          >
            <Icon name='paperclip' />
          </Button>
          <input
            ref={fileInputRef}
            type='file'
            hidden
            onChange={(event) => {
              if (event.target.files && event.target.files[0]) {
                onUpload(event.target.files[0]);
              }
              event.target.value = '';
            }}
          />
        </div>
        <TextArea
          value={value}
          onChange={(event, data) => onChange(String(data.value ?? ''))}
          onKeyDown={handleKeyDown}
          placeholder={t('chat.inputPlaceholder') || ''}
          style={{ flex: 1, minHeight: 64, width: '100%' }}
        />
        {streaming ? (
          <Button color='red' onClick={onStop} title={t('chat.stop') || ''}>
            <Icon name='stop' /> {t('chat.stop')}
          </Button>
        ) : (
          <Button color='blue' onClick={onSend} disabled={!value.trim() && selectedFileIds.length === 0}>
            <Icon name='send' /> {t('chat.send')}
          </Button>
        )}
      </div>
      <div style={{ marginTop: 6, display: 'flex', alignItems: 'center', gap: 8 }}>
        <Dropdown
          multiple
          selection
          size='small'
          placeholder={t('chat.selectSkills') || ''}
          options={skillOptions}
          value={selectedSkillIds}
          onChange={(event, data) => {
            const next = (data.value as number[]) || [];
            const added = next.filter((id) => !selectedSkillIds.includes(id));
            const removed = selectedSkillIds.filter((id) => !next.includes(id));
            added.forEach(onToggleSkill);
            removed.forEach(onToggleSkill);
          }}
          style={{ fontSize: '0.85em', minWidth: 220 }}
        />
      </div>
    </div>
  );
};

export default ChatComposer;
