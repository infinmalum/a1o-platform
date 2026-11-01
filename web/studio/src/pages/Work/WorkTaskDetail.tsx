import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Ban, Send } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Textarea } from '@/components/ui/textarea';
import {
  AgentRun,
  RunEvent,
  RunProjection,
  WorkTask,
  isRunActive,
  isRunTerminal,
  statusBadgeVariant,
  workApi,
} from './workApi';

// Work task detail (R3): the run list, the selected run's transcript, a live
// event timeline while the run executes, appended input, and cancel. The
// event stream is an EventSource over GET /api/studio/runs/:id/events — the
// server replays persisted events after a cursor and keeps streaming until
// the run is terminal; leaving the page never affects the run itself.
const WorkTaskDetail: React.FC = () => {
  const { t } = useTranslation();
  const { taskId } = useParams<{ taskId: string }>();
  const [task, setTask] = useState<WorkTask | null>(null);
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);
  const [projection, setProjection] = useState<RunProjection | null>(null);
  const [liveText, setLiveText] = useState('');
  const [activity, setActivity] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const streamRef = useRef<EventSource | null>(null);
  const activitySeqRef = useRef(0);
  const projectionRef = useRef<RunProjection | null>(null);

  const selectedRun = runs.find((run) => run.id === selectedRunId) || null;
  const active = selectedRun ? isRunActive(selectedRun.status) : false;

  const loadTask = useCallback(
    async (preferRunId?: number) => {
      if (!taskId) return;
      try {
        const data = await workApi.getTask(Number(taskId));
        setTask(data.task);
        setRuns(data.runs || []);
        const candidate =
          (preferRunId && data.runs?.find((run) => run.id === preferRunId)) ||
          data.runs?.[0] ||
          null;
        setSelectedRunId(candidate ? candidate.id : null);
      } catch {
        // toast already surfaced
      }
    },
    [taskId],
  );

  const loadProjection = useCallback(async (runId: number) => {
    try {
      const data = await workApi.getRun(runId);
      setProjection(data);
      projectionRef.current = data;
      setLiveText('');
      activitySeqRef.current = 0;
    } catch {
      // toast already surfaced
    }
  }, []);

  useEffect(() => {
    loadTask();
  }, [loadTask]);

  useEffect(() => {
    if (selectedRunId == null) {
      setProjection(null);
      return;
    }
    loadProjection(selectedRunId);
  }, [selectedRunId, loadProjection]);

  // Observe the selected run while it is not terminal: replay plus live
  // events until the server closes the stream, then reload the projection.
  useEffect(() => {
    if (!selectedRunId || !selectedRun || isRunTerminal(selectedRun.status)) {
      return;
    }
    const source = new EventSource(`/api/studio/runs/${selectedRunId}/events?after=0`);
    streamRef.current = source;
    source.onmessage = (message) => {
      let event: RunEvent;
      try {
        event = JSON.parse(message.data) as RunEvent;
      } catch {
        return;
      }
      switch (event.type) {
        case 'response.output_text.delta': {
          const delta = typeof event.payload?.delta === 'string' ? event.payload.delta : '';
          setLiveText((current) => current + delta);
          break;
        }
        case 'tool.called':
        case 'tool.result':
        case 'input.appended': {
          // Persisted events replay on every resubscription; the sequence
          // cursor keeps the timeline from duplicating entries.
          if ((event.sequence ?? 0) <= activitySeqRef.current) break;
          activitySeqRef.current = event.sequence ?? 0;
          const entry =
            event.type === 'tool.called'
              ? t('work.events.toolCalled', { name: String(event.payload?.name ?? '') })
              : event.type === 'tool.result'
                ? t('work.events.toolResult')
                : t('work.events.inputAppended');
          setActivity((current) => [...current, entry].slice(-30));
          break;
        }
        case 'run.completed':
        case 'run.failed':
        case 'run.cancelled':
        case 'run.needs_reconciliation':
        case 'run.waiting_input':
          source.close();
          streamRef.current = null;
          setLiveText('');
          setActivity([]);
          activitySeqRef.current = 0;
          loadTask(selectedRunId);
          loadProjection(selectedRunId);
          break;
        default:
          break;
      }
    };
    source.onerror = () => {
      // The stream ends on terminal state; only reconnect while the run is
      // still active so a proxy hiccup does not drop live progress.
      source.close();
      streamRef.current = null;
    };
    return () => {
      source.close();
      streamRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRunId, selectedRun?.status]);

  // A cancel_requested or terminal flip that never produced an SSE event
  // (cross-instance timing) still needs a UI refresh.
  useEffect(() => {
    if (!active || !selectedRunId) return;
    const timer = setInterval(() => {
      workApi
        .getRun(selectedRunId)
        .then((data) => {
          if (data.run.status !== projectionRef.current?.run.status) {
            loadTask(selectedRunId);
          }
          setProjection(data);
          projectionRef.current = data;
        })
        .catch(() => undefined);
    }, 3000);
    return () => clearInterval(timer);
  }, [active, selectedRunId, loadTask]);

  const submitInput = async () => {
    if (!selectedRunId || !input.trim() || sending) return;
    setSending(true);
    try {
      await workApi.appendInput(selectedRunId, input.trim());
      setInput('');
      loadProjection(selectedRunId);
    } catch {
      // toast already surfaced
    } finally {
      setSending(false);
    }
  };

  const cancel = async () => {
    if (!selectedRunId) return;
    try {
      await workApi.cancelRun(selectedRunId);
      loadTask(selectedRunId);
    } catch {
      // toast already surfaced
    }
  };

  if (!task) {
    return (
      <div className='mx-auto w-full max-w-4xl px-4 py-10'>
        <p className='text-muted-foreground text-sm'>{t('work.loading')}</p>
      </div>
    );
  }

  const messages = projection?.messages ?? [];
  const status = selectedRun?.status;

  return (
    <div className='mx-auto w-full max-w-4xl px-4 py-10'>
      <div className='mb-6'>
        <Link to='/work' className='text-muted-foreground inline-flex items-center gap-1 text-sm hover:underline'>
          <ArrowLeft className='size-3.5' /> {t('work.backToList')}
        </Link>
        <div className='mt-3 flex items-start justify-between gap-4'>
          <div>
            <h1 className='text-2xl font-semibold tracking-tight'>{task.title}</h1>
            <p className='text-muted-foreground mt-1 text-sm'>
              {new Date(task.created_time * 1000).toLocaleString()}
            </p>
          </div>
          {selectedRun && isRunActive(selectedRun.status) && (
            <Button variant='outline' size='sm' onClick={cancel}>
              <Ban /> {t('work.cancel')}
            </Button>
          )}
        </div>
        {task.summary && (
          <Card className='mt-4'>
            <CardHeader>
              <CardTitle className='text-sm'>{t('work.summary')}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className='whitespace-pre-wrap text-sm'>{task.summary}</p>
            </CardContent>
          </Card>
        )}
      </div>

      {runs.length > 1 && (
        <div className='mb-4 flex flex-wrap gap-2'>
          {runs.map((run) => (
            <Button
              key={run.id}
              size='sm'
              variant={run.id === selectedRunId ? 'default' : 'outline'}
              onClick={() => setSelectedRunId(run.id)}
            >
              #{run.id}
              <Badge variant={statusBadgeVariant(run.status)}>
                {t(`work.status.${run.status}`)}
              </Badge>
            </Button>
          ))}
        </div>
      )}

      <Card>
        <CardHeader>
          <div className='flex items-center justify-between'>
            <CardTitle className='text-base'>{t('work.progress')}</CardTitle>
            {status && (
              <div className='flex items-center gap-2'>
                <Badge variant={statusBadgeVariant(status)}>{t(`work.status.${status}`)}</Badge>
                {projection && (
                  <span className='text-muted-foreground text-xs'>
                    {t('work.usage', {
                      prompt: projection.prompt_tokens,
                      completion: projection.completion_tokens,
                    })}
                  </span>
                )}
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent className='space-y-4'>
          {messages.length === 0 && !liveText ? (
            <p className='text-muted-foreground text-sm'>
              {status === 'queued' ? t('work.queuedHint') : t('work.noProgress')}
            </p>
          ) : (
            messages.map((message, index) => (
              <div
                key={index}
                className={
                  message.role === 'user'
                    ? 'ml-auto max-w-[85%] rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground'
                    : 'max-w-[85%] rounded-lg bg-muted px-3 py-2 text-sm'
                }
              >
                <p className='whitespace-pre-wrap'>{message.text}</p>
              </div>
            ))
          )}
          {liveText && (
            <div className='max-w-[85%] rounded-lg bg-muted px-3 py-2 text-sm'>
              <p className='chat-markdown whitespace-pre-wrap'>
                {liveText}
                <span className='chat-blink'>▍</span>
              </p>
            </div>
          )}
          {activity.length > 0 && (
            <div className='text-muted-foreground space-y-1 text-xs'>
              {activity.map((entry, index) => (
                <p key={index}>· {entry}</p>
              ))}
            </div>
          )}
          {status === 'needs_reconciliation' && selectedRun?.error && (
            <p className='text-destructive text-sm'>{selectedRun.error}</p>
          )}
          {status === 'failed' && selectedRun?.error && (
            <p className='text-destructive text-sm'>{selectedRun.error}</p>
          )}
        </CardContent>
      </Card>

      <div className='mt-4'>
        <Textarea
          value={input}
          onChange={(event) => setInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              submitInput();
            }
          }}
          placeholder={
            active
              ? t('work.inputPlaceholder')
              : status === 'waiting_input'
                ? t('work.inputPlaceholder')
                : t('work.inputDisabled')
          }
          rows={3}
          disabled={!active}
        />
        <div className='mt-2 flex items-center justify-between'>
          <span className='text-muted-foreground text-xs'>
            {projection && projection.pending_inputs > 0
              ? t('work.pendingInputs', { count: projection.pending_inputs })
              : t('work.inputHint')}
          </span>
          <Button size='sm' onClick={submitInput} disabled={!active || !input.trim() || sending}>
            <Send /> {t('work.sendInput')}
          </Button>
        </div>
      </div>
    </div>
  );
};

export default WorkTaskDetail;
