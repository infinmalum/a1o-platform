import React from 'react';
import { useTranslation } from 'react-i18next';
import { Label, Segment } from 'semantic-ui-react';
import DOMPurify from 'dompurify';
import { marked } from 'marked';
import type { ChatMessage } from './chatApi';

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
      <div style={{ margin: '8px 0 8px 48px' }}>
        <Segment
          basic
          compact
          style={{
            background: result?.isError ? '#fff6f6' : '#f8f8f8',
            border: '1px solid #e8e8e8',
            fontSize: '0.85em',
            whiteSpace: 'pre-wrap',
            maxHeight: 240,
            overflow: 'auto',
          }}
        >
          <Label size='tiny' color={result?.isError ? 'red' : 'grey'}>
            {result?.isError ? t('chat.toolFailed') : t('chat.toolResult')} · {result?.name}
          </Label>
          <div style={{ marginTop: 6 }}>{message.content}</div>
        </Segment>
      </div>
    );
  }
  if (message.compacted) {
    return (
      <div style={{ textAlign: 'center', margin: '8px 0' }}>
        <Label size='tiny' color='teal'>
          {t('chat.compacted')}
        </Label>
      </div>
    );
  }
  const isUser = message.role === 'user';
  return (
    <div style={{ display: 'flex', justifyContent: isUser ? 'flex-end' : 'flex-start', margin: '12px 0' }}>
      <div
        style={{
          maxWidth: '82%',
          padding: '10px 14px',
          borderRadius: 10,
          background: isUser ? '#2185d0' : '#f4f4f4',
          color: isUser ? '#fff' : '#1b1c1d',
        }}
      >
        {message.attachments && message.attachments.length > 0 && (
          <div style={{ marginBottom: 6 }}>
            {message.attachments.map((file) => (
              <Label
                key={file.id}
                size='tiny'
                color={isUser ? 'blue' : 'grey'}
                basic={!isUser}
                icon={file.kind === 'image' ? 'image' : 'file alternate'}
              >
                {file.name}
              </Label>
            ))}
          </div>
        )}
        {isUser ? (
          <div style={{ whiteSpace: 'pre-wrap' }}>{message.content}</div>
        ) : (
          <div
            className='chat-markdown'
            style={{ overflowWrap: 'anywhere' }}
            dangerouslySetInnerHTML={{ __html: renderMarkdown(message.content || '') }}
          />
        )}
        {message.toolCalls && message.toolCalls.length > 0 && (
          <div style={{ marginTop: 8 }}>
            {message.toolCalls.map((call) => (
              <Label key={call.id} size='tiny' basic color='orange'>
                <i aria-hidden className='wrench icon' />
                {call.name}
              </Label>
            ))}
          </div>
        )}
        {message.error && (
          <div style={{ marginTop: 6, color: '#db2828', fontSize: '0.85em' }}>
            {message.error}
          </div>
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
  const rendered = messages;
  return (
    <div style={{ flex: 1, overflowY: 'auto', padding: '12px 16px' }}>
      {rendered.length === 0 && (
        <div style={{ textAlign: 'center', color: '#888', marginTop: 80 }}>{emptyHint}</div>
      )}
      {rendered.map((message, index) => (
        <MessageBubble key={message.id || `m-${index}`} message={message} />
      ))}
      <div ref={bottomRef} />
    </div>
  );
};

export default ChatMessageList;
