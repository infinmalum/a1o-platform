import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Ban, RotateCcw, Send } from 'lucide-react';

import { formatStudioDate } from '@/helpers/render';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
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

const WorkTaskDetailView: React.FC<{ taskId: string | undefined }> = ({ taskId }) => {
  const { t } = useTranslation();
  const [task, setTask] = useState<WorkTask | null>(null);
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);
  const [projection, setProjection] = useState<RunProjection | null>(null);
  const [liveText, setLiveText] = useState('');
  const [activity, setActivity] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [resumeDialogOpen, setResumeDialogOpen] = useState(false);
  const [resuming, setResuming] = useState(false);
  // cursorRef is the event cursor sent as ?after= on (re)connect; it advances
  // with every sequenced envelope so a reconnect never replays what the
  // client already showed. activitySeqRef dedupes replayed activity entries.
  const cursorRef = useRef(0);
  const activitySeqRef = useRef(0);
  // What the current observation covers: switching runs, or observing a run
  // again after a terminal state (resume), resets the stream state — a
  // resumed run whose event log was pruned may even restart sequences at 1.
  const observedRunRef = useRef<number | null>(null);
  const observedTerminalRef = useRef(false);
  const projectionRef = useRef<RunProjection | null>(null);
  const selectedRunRef = useRef<number | null>(null);
  const projectionRequest = useRef<AbortController | null>(null);
  const taskRequest = useRef<AbortController | null>(null);

  const selectedRun = runs.find((run) => run.id === selectedRunId) || null;
  const status = selectedRun?.status;
  const active = status ? isRunActive(status) : false;

  const selectRun = useCallback((runId: number | null) => {
    if (selectedRunRef.current === runId) return;
    selectedRunRef.current = runId;
    projectionRequest.current?.abort();
    projectionRef.current = null;
    setProjection(null);
    setLiveText('');
    setActivity([]);
    setSelectedRunId(runId);
  }, []);

  const loadTask = useCallback(
    async () => {
      if (!taskId) return;
      taskRequest.current?.abort();
      const request = new AbortController();
      taskRequest.current = request;
      try {
        const data = await workApi.getTask(Number(taskId), request.signal);
        if (request.signal.aborted) return;
        setTask(data.task);
        const nextRuns = data.runs ?? [];
        setRuns(nextRuns);
        if (!nextRuns.some((run) => run.id === selectedRunRef.current)) {
          selectRun(nextRuns[0]?.id ?? null);
        }
      } catch {
        // toast already surfaced
      }
    },
    [taskId, selectRun],
  );

  const loadProjection = useCallback(async (runId: number, polling = false) => {
    if (selectedRunRef.current !== runId || (polling && projectionRequest.current)) return;
    projectionRequest.current?.abort();
    const request = new AbortController();
    projectionRequest.current = request;
    try {
      const data = await workApi.getRun(runId, request.signal, !polling);
      if (request.signal.aborted) return;
      if (polling && projectionRef.current?.run.status !== data.run.status) void loadTask();
      setProjection(data);
      projectionRef.current = data;
    } catch {
      // Interactive requests report their own errors. Polling retries on the next tick.
    } finally {
      if (projectionRequest.current === request) projectionRequest.current = null;
    }
  }, [loadTask]);

  useEffect(() => {
    void loadTask();
    return () => {
      taskRequest.current?.abort();
      projectionRequest.current?.abort();
    };
  }, [loadTask]);

  useEffect(() => {
    if (selectedRunId !== null) void loadProjection(selectedRunId);
  }, [selectedRunId, loadProjection]);

  // Observe the selected run while it is not terminal: replay plus live
  // events, with cursor-based resume and backoff reconnects so a proxy
  // hiccup does not drop live progress. Leaving the page never affects the
  // run itself.
  useEffect(() => {
    if (!selectedRunId || !status) {
      return;
    }
    if (isRunTerminal(status)) {
      observedTerminalRef.current = true;
      if (observedRunRef.current !== selectedRunId) {
        // Switching to a terminal run: drop the previous observation's
        // transient stream state; the persisted view lives in the projection.
        cursorRef.current = 0;
        activitySeqRef.current = 0;
        setLiveText('');
        setActivity([]);
        observedRunRef.current = selectedRunId;
      }
      return;
    }
    if (observedRunRef.current !== selectedRunId || observedTerminalRef.current) {
      // A fresh observation — another run, or this run re-activating after a
      // terminal state (resume) — resets the stream state: cursors, the live
      // delta buffer, and the timeline.
      cursorRef.current = 0;
      activitySeqRef.current = 0;
      setLiveText('');
      setActivity([]);
    }
    observedRunRef.current = selectedRunId;
    observedTerminalRef.current = false;
    let observing = true;

    let source: EventSource | null = null;
    let timer: number | null = null;
    let backoffMs = 1000;

    const close = () => {
      source?.close();
      source = null;
      if (timer != null) {
        window.clearTimeout(timer);
        timer = null;
      }
    };

    const connect = () => {
      // The server replays persisted events after the cursor and keeps
      // streaming until the run is terminal.
      source = new EventSource(`/api/studio/runs/${selectedRunId}/events?after=${cursorRef.current}`);
      source.onmessage = (message) => {
        if (!observing || selectedRunRef.current !== selectedRunId) return;
        backoffMs = 1000;
        let event: RunEvent;
        try {
          event = JSON.parse(message.data) as RunEvent;
        } catch {
          return;
        }
        if ((event.sequence ?? 0) > cursorRef.current) {
          cursorRef.current = event.sequence ?? 0;
        }
        switch (event.type) {
          case 'response.output_text.delta': {
            const delta = typeof event.payload?.delta === 'string' ? event.payload.delta : '';
            setLiveText((current) => current + delta);
            break;
          }
          case 'run.resync': {
            // A resumed run can restart its sequence numbers after log pruning.
            if (event.payload?.reset === true) {
              cursorRef.current =
                typeof event.payload?.latest_sequence === 'number'
                  ? event.payload.latest_sequence
                  : 0;
              activitySeqRef.current = 0;
              setActivity([]);
              setLiveText('');
            }
            loadProjection(selectedRunId);
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
            close();
            observing = false;
            setLiveText('');
            setActivity([]);
            activitySeqRef.current = 0;
            loadTask();
            loadProjection(selectedRunId);
            break;
          default:
            break;
        }
      };
      source.onerror = () => {
        // The server closes the stream on terminal state, and EventSource
        // surfaces clean closes as errors too; only reconnect while the run
        // is still active, with exponential backoff (reset on any message).
        close();
        if (!observing || selectedRunRef.current !== selectedRunId) return;
        timer = window.setTimeout(() => {
          timer = null;
          connect();
        }, backoffMs);
        backoffMs = Math.min(backoffMs * 2, 15000);
      };
    };
    connect();

    return () => {
      observing = false;
      close();
    };
  }, [selectedRunId, status, loadTask, loadProjection, t]);

  // A cancel_requested or terminal flip that never produced an SSE event
  // (cross-instance timing) still needs a UI refresh.
  useEffect(() => {
    if (!active || !selectedRunId) return;
    const timer = setInterval(() => void loadProjection(selectedRunId, true), 3000);
    return () => clearInterval(timer);
  }, [active, selectedRunId, loadProjection]);

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
      loadTask();
    } catch {
      // toast already surfaced
    }
  };

  // Resume continues a needs_reconciliation run from its persisted context;
  // the dialog's forensics come from the projection.
  const resume = async () => {
    if (!selectedRunId) return;
    setResuming(true);
    try {
      await workApi.resumeRun(selectedRunId);
      setResumeDialogOpen(false);
      loadTask();
      loadProjection(selectedRunId);
    } catch {
      // toast already surfaced
    } finally {
      setResuming(false);
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

  return (
    <div className='mx-auto w-full max-w-4xl px-4 py-10'>
      <div className='mb-6'>
        <Link to='/work' className='text-muted-foreground inline-flex items-center gap-1 text-sm hover:underline'>
          <ArrowLeft className='size-3.5 rtl:rotate-180' /> {t('work.backToList')}
        </Link>
        <div className='mt-3 flex items-start justify-between gap-4'>
          <div>
            <h1 className='text-2xl font-semibold tracking-tight'>{task.title}</h1>
            <p className='text-muted-foreground mt-1 text-sm'>
              {formatStudioDate(task.created_time)}
            </p>
          </div>
          <div className='flex shrink-0 items-center gap-2'>
            {selectedRun && isRunActive(selectedRun.status) && (
              <Button variant='outline' size='sm' onClick={cancel}>
                <Ban /> {t('work.cancel')}
              </Button>
            )}
            {status === 'needs_reconciliation' && (
              <Button size='sm' onClick={() => setResumeDialogOpen(true)}>
                <RotateCcw /> {t('work.resume')}
              </Button>
            )}
          </div>
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
              onClick={() => selectRun(run.id)}
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
                    ? 'ms-auto max-w-[85%] rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground'
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
          {(status === 'needs_reconciliation' || status === 'failed') && selectedRun?.error && (
            <p className='text-destructive text-sm'>{t(status === 'needs_reconciliation' ? 'work.reconciliationHint' : 'work.safeRunError')}</p>
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

      <Dialog open={resumeDialogOpen} onOpenChange={setResumeDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('work.resumeTitle')}</DialogTitle>
            <DialogDescription>{t('work.resumeDescription')}</DialogDescription>
          </DialogHeader>
          {(projection?.unknown_invocations ?? 0) > 0 && (
            <p className='text-destructive text-sm'>
              {t('work.resumeUnknownInvocations', { count: projection?.unknown_invocations })}
            </p>
          )}
          {(projection?.unverified_tools?.length ?? 0) > 0 && (
            <p className='text-destructive text-sm'>
              {t('work.resumeUnverifiedTools', {
                tools: projection?.unverified_tools.join(', '),
              })}
            </p>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setResumeDialogOpen(false)}>
              {t('work.resumeCancel')}
            </Button>
            <Button onClick={resume} disabled={resuming}>
              <RotateCcw /> {t('work.resumeConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default function WorkTaskDetail() {
  const { taskId } = useParams<{ taskId: string }>();
  return <WorkTaskDetailView key={taskId} taskId={taskId} />;
}
