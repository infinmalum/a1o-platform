import React from 'react';
import { useTranslation } from 'react-i18next';
import { Paperclip, Send, Square, X } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Textarea } from '@/components/ui/textarea';
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

  const enabledSkills = skills.filter((skill) => skill.enabled);

  return (
    <div className='border-t px-4 pt-2.5 pb-3.5'>
      {(selectedFileIds.length > 0 || selectedSkillIds.length > 0) && (
        <div className='mb-2 flex flex-wrap gap-1'>
          {selectedFileIds.map((id) => {
            const file = files.find((item) => item.id === id);
            return (
              <Badge key={`f-${id}`} variant='secondary' onClick={() => onToggleFile(id)}>
                <X className='size-3' /> {file ? file.name : `#${id}`}
              </Badge>
            );
          })}
          {selectedSkillIds.map((id) => {
            const skill = skills.find((item) => item.id === id);
            return (
              <Badge key={`s-${id}`} variant='outline' onClick={() => onToggleSkill(id)}>
                <X className='size-3' /> {skill ? skill.name : `#${id}`}
              </Badge>
            );
          })}
        </div>
      )}
      <div className='flex items-end gap-2'>
        <div>
          <Button
            variant='ghost'
            size='icon-sm'
            title={t('chat.attachFile')}
            onClick={() => fileInputRef.current?.click()}
          >
            <Paperclip />
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
        <Textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t('chat.inputPlaceholder')}
          className='min-h-16 flex-1'
        />
        {streaming ? (
          <Button variant='destructive' onClick={onStop} title={t('chat.stop')}>
            <Square /> {t('chat.stop')}
          </Button>
        ) : (
          <Button
            onClick={onSend}
            disabled={!value.trim() && selectedFileIds.length === 0}
          >
            <Send /> {t('chat.send')}
          </Button>
        )}
      </div>
      <div className='mt-2'>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant='outline' size='sm'>
              {t('chat.selectSkills')}
              {selectedSkillIds.length > 0 && ` (${selectedSkillIds.length})`}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align='start' className='w-64'>
            {enabledSkills.length === 0 && (
              <div className='text-muted-foreground px-2 py-1.5 text-sm'>
                {t('chat.noSkills')}
              </div>
            )}
            {enabledSkills.map((skill) => (
              <DropdownMenuCheckboxItem
                key={skill.id}
                checked={selectedSkillIds.includes(skill.id)}
                onCheckedChange={() => onToggleSkill(skill.id)}
                onSelect={(event) => event.preventDefault()}
              >
                {skill.name}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
};

export default ChatComposer;
