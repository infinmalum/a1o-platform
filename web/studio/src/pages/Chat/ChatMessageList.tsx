import React from 'react';
import { useTranslation } from 'react-i18next';
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import { Wrench } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

export interface DisplayToolCall {
  id: string;
  name: string;
  arguments: string;
}

export interface DisplayToolResult {
  id: string;
  name: string;
  content: string;
  isError: boolean;
}

export interface DisplayAttachment {
  id: number;
  name: string;
  kind: 'text' | 'image';
}

export interface DisplayMessage {
  id: number;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  streaming?: boolean;
  error?: string;
  toolCalls?: DisplayToolCall[];
  toolResult?: DisplayToolResult;
  attachments?: DisplayAttachment[];
  compacted?: boolean;
}

// renderMarkdown sanitizes model output before it enters the DOM.
function renderMarkdown(text: string): string {
  const html = marked.parse(text, { async: false }) as string;
  return DOMPurify.sanitize(html);
}

const MessageBubble: React.FC<{ message: DisplayMessage }> = ({ message }) => {
  const { t } = useTranslation();
  if (message.role === 'tool') {
    const result = message.toolResult;
    return (
      <div className='my-2 ml-12'>
        <div
          className={cn(
            'max-h-60 overflow-auto rounded-md border p-3 text-sm whitespace-pre-wrap',
            result?.isError ? 'border-destructive/30 bg-destructive/5' : 'bg-muted/60'
          )}
        >
          <Badge variant={result?.isError ? 'destructive' : 'secondary'} className='mb-2'>
            {result?.isError ? t('chat.toolFailed') : t('chat.toolResult')} · {result?.name}
          </Badge>
          <div>{message.content}</div>
        </div>
      </div>
    );
  }
  if (message.compacted) {
    return (
      <div className='my-2 text-center'>
        <Badge variant='outline' className='text-muted-foreground'>
          {t('chat.compacted')}
        </Badge>
      </div>
    );
  }
  const isUser = message.role === 'user';
  return (
    <div
      className={cn('my-3 flex', isUser ? 'justify-end' : 'justify-start')}
    >
      <div
        className={cn(
          'max-w-[82%] rounded-2xl px-4 py-2.5 text-sm',
          isUser ? 'bg-primary text-primary-foreground' : 'bg-muted'
        )}
      >
        {message.attachments && message.attachments.length > 0 && (
          <div className='mb-2 flex flex-wrap gap-1'>
            {message.attachments.map((file) => (
              <Badge
                key={file.id}
                variant={isUser ? 'secondary' : 'outline'}
                className={isUser ? 'bg-primary-foreground/15 text-primary-foreground' : ''}
              >
                {file.kind === 'image' ? '🖼' : '📄'} {file.name}
              </Badge>
            ))}
          </div>
        )}
        {isUser ? (
          <div className='whitespace-pre-wrap'>{message.content}</div>
        ) : (
          <div
            className='chat-markdown overflow-wrap:anywhere'
            dangerouslySetInnerHTML={{ __html: renderMarkdown(message.content || '') }}
          />
        )}
        {message.toolCalls && message.toolCalls.length > 0 && (
          <div className='mt-2 flex flex-wrap gap-1'>
            {message.toolCalls.map((call) => (
              <Badge key={call.id} variant='outline'>
                <Wrench className='size-3' /> {call.name}
              </Badge>
            ))}
          </div>
        )}
        {message.error && (
          <div className='mt-2 text-xs text-destructive'>{message.error}</div>
        )}
        {message.streaming && <span className='chat-cursor'>▍</span>}
      </div>
    </div>
  );
};

const ChatMessageList: React.FC<{
  messages: DisplayMessage[];
  emptyHint: string;
}> = ({ messages, emptyHint }) => {
  const bottomRef = React.useRef<HTMLDivElement>(null);
  React.useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);
  return (
    <div className='flex-1 overflow-y-auto px-4 py-3'>
      {messages.length === 0 && (
        <div className='text-muted-foreground mt-20 text-center text-sm'>{emptyHint}</div>
      )}
      {messages.map((message, index) => (
        <MessageBubble key={message.id || `m-${index}`} message={message} />
      ))}
      <div ref={bottomRef} />
    </div>
  );
};

export default ChatMessageList;
