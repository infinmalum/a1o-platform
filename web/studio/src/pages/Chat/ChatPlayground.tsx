import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FlaskConical } from 'lucide-react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { renderQuota } from '@/helpers/render';
import type { ChatSession } from './chatApi';
import {
  buildRequest, exportCurl, getPlaygroundUsage, protocols, requestTemplate, runPlayground,
  type PlaygroundRequest, type PlaygroundUsage, type Protocol,
} from './playgroundApi';

const maxPreviewCharacters = 256 * 1024;

type RunResult = {
  model: string;
  request: PlaygroundRequest;
  output?: string;
  status?: number;
  elapsed?: number;
  truncated?: boolean;
  error?: string;
  stopped?: boolean;
  usage?: PlaygroundUsage;
  requestId?: string;
};

type Props = {
  onSettled: () => void;
  models: string[];
  currentModel: string;
  session: ChatSession | null;
  input: string;
};

export default function ChatPlayground({ onSettled, models, currentModel, session, input }: Props) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [protocol, setProtocol] = useState<Protocol>('chat');
  const [drafts, setDrafts] = useState<Partial<Record<Protocol, string>>>({});
  const [selectedModel, setSelectedModel] = useState('');
  const [comparisonModel, setComparisonModel] = useState('');
  const [geminiStream, setGeminiStream] = useState(false);
  const [results, setResults] = useState<RunResult[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => () => abortRef.current?.abort(), []);

  const model = models.includes(selectedModel)
    ? selectedModel
    : (models.includes(currentModel) ? currentModel : models[0] ?? '');
  const comparison = models.includes(comparisonModel) && comparisonModel !== model ? comparisonModel : '';
  const example = t('playground.examplePrompt');
  const source = drafts[protocol] ?? JSON.stringify(requestTemplate(protocol, example), null, 2);
  const draftRequest = useMemo(() => {
    if (!open || !model) return undefined;
    try {
      return buildRequest(protocol, model, source, geminiStream);
    } catch {
      // The editor may contain incomplete JSON while typing.
      return undefined;
    }
  }, [open, model, protocol, source, geminiStream]);

  const setSource = (value: string) => {
    setDrafts((previous) => ({ ...previous, [protocol]: value }));
    setError('');
  };

  const start = async () => {
    if (!model || abortRef.current) return;
    if (!draftRequest) {
      setError(t('playground.invalidJSON'));
      return;
    }
    const runs: RunResult[] = [{ model, request: draftRequest }];
    if (comparison) {
      runs.push({ model: comparison, request: buildRequest(protocol, comparison, source, geminiStream) });
    }

    const abort = new AbortController();
    abortRef.current = abort;
    setError('');
    setResults(runs);
    setRunning(true);

    const update = (index: number, changes: Partial<RunResult>) => {
      setResults((previous) => previous.map((run, position) =>
        position === index ? { ...run, ...changes } : run,
      ));
    };

    try {
      // Compare identical inputs without making a small deployment handle
      // two upstream requests and billing reservations at once.
      for (const [index, run] of runs.entries()) {
        if (abort.signal.aborted) {
          update(index, { stopped: true });
          break;
        }
        const started = performance.now();
        const elapsed = () => Math.round(performance.now() - started);
        try {
          const response = await runPlayground(run.request, abort.signal);
          const requestId = response.headers.get('X-A1O-Request-Id') ?? undefined;
          update(index, { status: response.status, requestId });
          const reader = response.body?.getReader();
          const decoder = new TextDecoder();
          let output = '';
          let truncated = false;
          let lastUpdate = started;
          const append = (text: string) => {
            const remaining = maxPreviewCharacters - output.length;
            truncated ||= text.length > remaining;
            if (remaining > 0) output += text.slice(0, remaining);
          };
          if (reader) {
            try {
              for (;;) {
                const { value, done } = await reader.read();
                if (done) break;
                append(decoder.decode(value, { stream: true }));
                if (performance.now() - lastUpdate > 100) {
                  update(index, { output, truncated });
                  lastUpdate = performance.now();
                }
              }
              append(decoder.decode());
            } catch (cause) {
              update(index, { output, truncated });
              throw cause;
            } finally {
              reader.releaseLock();
            }
          }
          update(index, { output, truncated, elapsed: elapsed() });

          if (requestId && response.ok && response.status !== 202) {
            try {
              const usage = await getPlaygroundUsage(requestId, abort.signal);
              if (usage) update(index, { usage });
            } catch {
              // The completed response is usable when usage is unavailable.
            }
          }
        } catch {
          update(index, abort.signal.aborted
            ? { stopped: true, elapsed: elapsed() }
            : { error: t('playground.requestFailed'), elapsed: elapsed() });
        }
      }
    } finally {
      abortRef.current = null;
      setRunning(false);
      onSettled();
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => {
      if (!next) abortRef.current?.abort();
      setOpen(next);
    }}>
      <DialogTrigger asChild>
        <Button size='sm' variant='outline' aria-label={t('playground.title')}>
          <FlaskConical /> <span className='hidden sm:inline'>{t('playground.title')}</span>
        </Button>
      </DialogTrigger>
      <DialogContent className='max-h-[90dvh] overflow-y-auto sm:max-w-5xl'>
        <DialogHeader>
          <DialogTitle>{t('playground.title')}</DialogTitle>
          <DialogDescription>{t('playground.description')}</DialogDescription>
        </DialogHeader>
        <div className='grid gap-3 sm:grid-cols-3'>
          <div className='grid min-w-0 gap-1.5'>
            <Label htmlFor='playground-protocol'>{t('playground.protocol')}</Label>
            <Select
              value={protocol}
              onValueChange={(value) => { setProtocol(value as Protocol); setError(''); }}
              disabled={running}
            >
              <SelectTrigger id='playground-protocol' className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent>
                {(Object.entries(protocols) as [Protocol, string][]).map(([value, label]) => (
                  <SelectItem key={value} value={value}>{label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid min-w-0 gap-1.5'>
            <Label htmlFor='playground-model'>{t('chat.model')}</Label>
            <Select value={model} onValueChange={setSelectedModel} disabled={running || models.length === 0}>
              <SelectTrigger id='playground-model' className='w-full'>
                <SelectValue placeholder={t('playground.noModels')} />
              </SelectTrigger>
              <SelectContent className='max-h-72'>
                {models.map((name) => <SelectItem key={name} value={name}>{name}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className='grid min-w-0 gap-1.5'>
            <Label htmlFor='playground-compare'>{t('playground.compare')}</Label>
            <Select
              value={comparison ? `model:${comparison}` : 'off'}
              onValueChange={(value) => setComparisonModel(value === 'off' ? '' : value.slice(6))}
              disabled={running || models.length < 2}
            >
              <SelectTrigger id='playground-compare' className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent className='max-h-72'>
                <SelectItem value='off'>{t('playground.noComparison')}</SelectItem>
                {models.filter((name) => name !== model).map((name) => (
                  <SelectItem key={name} value={`model:${name}`}>{name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        {comparison && <p className='text-muted-foreground text-xs'>{t('playground.compareHint')}</p>}
        <div className='grid min-w-0 gap-2'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <Label htmlFor='playground-body'>{t('playground.request')}</Label>
            <Button
              size='sm'
              variant='outline'
              disabled={running || (!session && !input.trim())}
              onClick={() => {
                setSource(JSON.stringify(requestTemplate(
                  protocol, input.trim() || example, session?.system_prompt, session ?? undefined,
                ), null, 2));
              }}
            >
              {t('playground.useChatDraft')}
            </Button>
          </div>
          <Textarea
            id='playground-body'
            dir='ltr'
            spellCheck={false}
            value={source}
            onChange={(event) => setSource(event.target.value)}
            rows={10}
            disabled={running}
            aria-describedby='playground-body-hint'
            className='max-h-[35dvh] min-h-32 font-mono text-xs sm:text-sm'
          />
          <p id='playground-body-hint' className='text-muted-foreground text-xs'>
            {t(protocol === 'gemini' ? 'playground.geminiHint' : 'playground.requestHint')}
          </p>
          {protocol === 'gemini' && (
            <div className='flex items-center gap-2'>
              <Switch id='playground-stream' checked={geminiStream} onCheckedChange={setGeminiStream} disabled={running} />
              <Label htmlFor='playground-stream'>{t('playground.streaming')}</Label>
            </div>
          )}
          {error && <p role='alert' className='text-destructive text-sm'>{error}</p>}
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button onClick={start} disabled={running || !model}>
            {running ? t('playground.running') : t('playground.run')}
          </Button>
          {running && <Button variant='outline' onClick={() => abortRef.current?.abort()}>{t('chat.stop')}</Button>}
        </div>
        {draftRequest && !results.some(({ request }) => request.path === draftRequest.path && request.body === draftRequest.body) && (
          <CurlDetails request={draftRequest} />
        )}
        {results.length > 0 && (
          <div className={`grid min-w-0 gap-3 ${results.length > 1 ? 'md:grid-cols-2' : ''}`}>
            {results.map((result) => <Result key={result.model} result={result} />)}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function Result({ result }: { result: RunResult }) {
  const { t } = useTranslation();
  const { model, request, status, elapsed, output, stopped, truncated, error, usage, requestId } = result;
  return (
    <section className='grid min-w-0 content-start gap-2 rounded-md border p-3'>
      <h3 className='break-all text-sm font-medium'>{model}</h3>
      <p className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs' role='status'>
        {status !== undefined && <span>HTTP {status}</span>}
        {elapsed !== undefined && <span>{elapsed} ms</span>}
        {stopped && <span>{t('playground.stopped')}</span>}
        {usage ? (
          <>
            <span>{t('playground.tokens', { input: usage.prompt_tokens, output: usage.completion_tokens })}</span>
            <span>{t('playground.cost', { amount: renderQuota(usage.quota, t, 6) })}</span>
          </>
        ) : elapsed !== undefined && <span>{t('playground.usageUnavailable')}</span>}
      </p>
      {error && <p role='alert' className='text-destructive text-xs'>{error}</p>}
      {truncated && <p className='text-muted-foreground text-xs'>{t('playground.truncated')}</p>}
      {requestId && <p className='text-muted-foreground break-all text-xs'>{t('playground.requestId', { id: requestId })}</p>}
      <pre dir='ltr' aria-label={t('playground.response')} className='bg-muted max-h-72 min-h-24 overflow-auto whitespace-pre-wrap break-all rounded-md p-2 text-xs'>
        {output || (elapsed === undefined ? t('playground.waiting') : t('playground.emptyResponse'))}
      </pre>
      <CurlDetails request={request} />
    </section>
  );
}

function CurlDetails({ request }: { request: PlaygroundRequest }) {
  const { t } = useTranslation();
  const code = exportCurl(request, window.location.origin);
  return (
    <details className='min-w-0 text-xs'>
      <summary className='cursor-pointer'>{t('playground.showCurl')}</summary>
      <pre dir='ltr' className='bg-muted my-2 max-h-52 overflow-auto whitespace-pre-wrap break-all rounded-md p-2'>{code}</pre>
      <Button size='sm' variant='outline' onClick={async () => {
        try {
          await navigator.clipboard.writeText(code);
          toast.success(t('playground.copied'));
        } catch {
          toast.error(t('playground.copyFailed'));
        }
      }}>{t('playground.copyCurl')}</Button>
    </details>
  );
}
