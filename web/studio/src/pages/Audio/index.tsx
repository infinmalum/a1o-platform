import { studioFetch, requestError } from '@/helpers/studioRequest';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { chatApi } from '@/pages/Chat/chatApi';

const modes = ['speech', 'transcriptions', 'translations'] as const;
type Mode = typeof modes[number];
const maxAudioBytes = 16 * 1024 * 1024;
const maxSpeechBytes = 4096;

type AudioResult = { mode: Mode; url: string; text?: string };

export default function AudioStudio() {
  const { t } = useTranslation();
  const [mode, setMode] = useState<Mode>('speech');
  const [models, setModels] = useState<string[]>([]);
  const [selectedModels, setSelectedModels] = useState<Partial<Record<Mode, string>>>({});
  const [voice, setVoice] = useState('alloy');
  const [text, setText] = useState('');
  const [file, setFile] = useState<File>();
  const [result, setResult] = useState<AudioResult>();
  const [error, setError] = useState('');
  const [running, setRunning] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const speech = mode === 'speech';
  const suggested = models.filter((name) => speech ? /tts|speech/i.test(name) : /transcrib|whisper/i.test(name));
  const model = selectedModels[mode] ?? suggested[0] ?? '';

  useEffect(() => {
    let mounted = true;
    void chatApi.listModels().then((list) => { if (mounted) setModels(list); }).catch(() => undefined);
    return () => {
      mounted = false;
      abort.current?.abort();
    };
  }, []);

  useEffect(() => {
    return () => { if (result) URL.revokeObjectURL(result.url); };
  }, [result]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!model.trim() || abort.current || (speech ? !text.trim() || !voice.trim() : !file)) return;
    if (speech && new TextEncoder().encode(text.trim()).length > maxSpeechBytes) {
      setError(t('audio.textLimit'));
      return;
    }
    if (file && !speech && file.size > maxAudioBytes) {
      setError(t('audio.fileLimit'));
      return;
    }
    const controller = new AbortController();
    abort.current = controller;
    setError('');
    setRunning(true);
    setResult(undefined);
    try {
      let body: BodyInit;
      let headers: HeadersInit | undefined;
      if (speech) {
        headers = { 'Content-Type': 'application/json' };
        body = JSON.stringify({ model: model.trim(), voice: voice.trim(), input: text.trim(), response_format: 'mp3' });
      } else {
        const form = new FormData();
        form.set('model', model.trim());
        if (file) form.set('file', file);
        form.set('response_format', 'json');
        body = form;
      }
      const response = await studioFetch(`/api/studio/media/v1/audio/${mode}`, {
        method: 'POST', body, headers, signal: controller.signal,
      }, true);
      const contentType = response.headers.get('Content-Type') || '';
      if (!response.ok || contentType.includes('json') || !speech) {
        const payload = await response.json().catch(() => null);
        if (!response.ok || payload?.success === false || payload?.error) {
          console.error('Audio request failed', response.status, payload?.error?.code);
          throw requestError(response.status === 401 ? 502 : response.status, payload?.error?.code);
        }
        if (speech || typeof payload?.text !== 'string') throw new Error(t('audio.invalidResult'));
        setResult({ mode, text: payload.text, url: URL.createObjectURL(new Blob([payload.text], { type: 'text/plain;charset=utf-8' })) });
      } else {
        if (!contentType.startsWith('audio/') && !contentType.startsWith('application/octet-stream')) {
          throw new Error(t('audio.invalidResult'));
        }
        const sound = await response.blob();
        setResult({ mode, url: URL.createObjectURL(sound) });
      }
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : t('audio.invalidResult'));
    } finally {
      abort.current = null;
      setRunning(false);
    }
  };

  return (
    <div className='mx-auto grid w-full max-w-3xl gap-6 px-4 py-10'>
      <header className='flex flex-wrap items-center justify-between gap-3'>
        <h1 className='text-2xl font-semibold'>{t('audio.title')}</h1>
        <Button variant='outline' asChild><Link to='/images'>{t('images.title')}</Link></Button>
      </header>
      <Card>
        <CardContent>
          <form className='grid gap-4' onSubmit={submit}>
            <div className='grid gap-2 sm:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='audio-mode'>{t('audio.mode')}</Label>
                <Select value={mode} disabled={running} onValueChange={(value: Mode) => {
                  setMode(value);
                  setResult(undefined);
                  setError('');
                }}>
                  <SelectTrigger id='audio-mode' className='w-full'><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {modes.map((option) => <SelectItem key={option} value={option}>{t(`audio.${option}`)}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='audio-model'>{t('images.modelLabel')}</Label>
                <Input id='audio-model' list='audio-models' value={model} disabled={running}
                  placeholder={t('audio.modelPlaceholder')}
                  onChange={(event) => setSelectedModels((previous) => ({ ...previous, [mode]: event.target.value }))} />
                <datalist id='audio-models'>
                  {[...new Set([...suggested, ...models])].map((name) => <option key={name} value={name} />)}
                </datalist>
              </div>
            </div>
            {speech ? (
              <div className='grid gap-2'>
                <Label htmlFor='audio-voice'>{t('audio.voice')}</Label>
                <Input id='audio-voice' value={voice} maxLength={80} disabled={running} onChange={(event) => setVoice(event.target.value)} />
                <Label htmlFor='audio-text'>{t('audio.text')}</Label>
                <Textarea id='audio-text' rows={5} value={text} maxLength={maxSpeechBytes} disabled={running}
                  onChange={(event) => setText(event.target.value)} />
              </div>
            ) : (
              <div className='grid gap-2'>
                <Label htmlFor='audio-file'>{t('audio.file')}</Label>
                <Input id='audio-file' type='file' accept='audio/*,.m4a,.mp4,.webm' disabled={running}
                  onChange={(event) => setFile(event.target.files?.[0])} />
                {file && <p className='text-muted-foreground text-sm'>{file.name}</p>}
                <p className='text-muted-foreground text-xs'>{t('audio.fileLimit')}</p>
              </div>
            )}
            {error && <p role='alert' className='text-destructive text-sm'>{error}</p>}
            <div className='flex flex-wrap gap-2'>
              <Button type='submit' disabled={running || !model.trim() || (speech ? !text.trim() || !voice.trim() : !file)}>
                {t(running ? 'audio.running' : 'audio.run')}
              </Button>
              {running && <Button type='button' variant='outline' onClick={() => abort.current?.abort()}>{t('chat.stop')}</Button>}
            </div>
          </form>
        </CardContent>
      </Card>
      {result && (
        <Card>
          <CardContent className='grid justify-items-start gap-3'>
            {result.mode === 'speech' ? (
              <audio className='w-full' controls src={result.url} />
            ) : (
              <p className='whitespace-pre-wrap break-words text-sm'>{result.text}</p>
            )}
            <Button variant='outline' asChild>
              <a href={result.url} download={result.mode === 'speech' ? 'speech.mp3' : 'transcript.txt'}>{t('audio.download')}</a>
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
