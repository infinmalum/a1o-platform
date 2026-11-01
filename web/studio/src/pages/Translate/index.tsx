import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ArrowLeftRight, Check, Copy, Languages, Loader2 } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { translateApi, type TranslationRecord } from './translateApi';

type Engine = 'deepl' | 'llm';

// DeepL's language table for the source selector; the gateway auto-detects
// when the field stays empty.
const SOURCE_LANGUAGES = [
  'AUTO', 'ZH', 'EN', 'JA', 'KO', 'DE', 'FR', 'ES', 'PT', 'IT', 'RU', 'AR',
];

const formatUsage = (record: TranslationRecord, t: (key: string) => string): string => {
  if (record.engine === 'deepl') {
    return t('translate.usageCharacters').replace('{count}', String(record.billed_characters));
  }
  return t('translate.usageTokens')
    .replace('{input}', String(record.input_tokens))
    .replace('{output}', String(record.output_tokens));
};

// Translation workbench: two panes (source/target), explicit translate
// button (never a per-keystroke paid request), engine metadata driven
// selection, and the persisted history.
const Translate: React.FC = () => {
  const { t } = useTranslation();
  const [meta, setMeta] = useState<{ deepl_languages: string[]; models: string[] }>({
    deepl_languages: [],
    models: [],
  });
  const [engine, setEngine] = useState<Engine>('llm');
  const [sourceLang, setSourceLang] = useState('AUTO');
  const [targetLang, setTargetLang] = useState('');
  const [model, setModel] = useState('');
  const [input, setInput] = useState('');
  const [output, setOutput] = useState('');
  const [activeUsage, setActiveUsage] = useState('');
  const [detected, setDetected] = useState('');
  const [running, setRunning] = useState(false);
  const [copied, setCopied] = useState(false);
  const [history, setHistory] = useState<TranslationRecord[]>([]);

  const deeplAvailable = meta.deepl_languages.length > 0;

  useEffect(() => {
    translateApi
      .meta()
      .then((data) => {
        setMeta(data);
        if (data.deepl_languages.length > 0) {
          setEngine('deepl');
          setTargetLang(data.deepl_languages.includes('ZH') ? 'ZH' : data.deepl_languages[0]);
        }
      })
      .catch(() => undefined);
    translateApi.history().then(setHistory).catch(() => undefined);
  }, []);

  const swap = () => {
    if (sourceLang === 'AUTO' || !sourceLang) return;
    const previousTarget = targetLang;
    setTargetLang(sourceLang);
    setSourceLang(previousTarget || 'AUTO');
    setInput(output);
    setOutput(input);
    setActiveUsage('');
    setDetected('');
  };

  const translate = useCallback(async () => {
    if (!input.trim() || !targetLang || running) return;
    setRunning(true);
    setOutput('');
    setActiveUsage('');
    setDetected('');
    setCopied(false);
    try {
      const record = await translateApi.create({
        text: input,
        target_lang: targetLang,
        source_lang: sourceLang === 'AUTO' ? '' : sourceLang,
        engine,
        model: engine === 'llm' ? model : undefined,
      });
      setOutput(record.output_text);
      setDetected(record.detected_lang || '');
      setActiveUsage(formatUsage(record, t));
      const entries = await translateApi.history();
      setHistory(entries);
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setRunning(false);
    }
  }, [input, targetLang, sourceLang, engine, model, running, t]);

  const copyOutput = async () => {
    if (!output) return;
    try {
      await navigator.clipboard.writeText(output);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable; nothing to recover
    }
  };

  const statusBadge = (record: TranslationRecord) => {
    if (record.status === 'failed') {
      return <Badge variant='destructive'>{t('translate.failed')}</Badge>;
    }
    if (record.status === 'running') {
      return <Badge variant='secondary'>{t('translate.running')}</Badge>;
    }
    return null;
  };

  const historyEntries = useMemo(() => history.slice(0, 10), [history]);

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-10'>
      <div className='mb-8 flex items-center justify-between'>
        <div>
          <h1 className='flex items-center gap-2 text-2xl font-semibold tracking-tight'>
            <Languages className='h-6 w-6' /> {t('translate.title')}
          </h1>
          <p className='text-muted-foreground mt-1 text-sm'>{t('translate.tagline')}</p>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Select value={engine} onValueChange={(value) => setEngine(value as Engine)}>
            <SelectTrigger className='w-40'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='deepl' disabled={!deeplAvailable}>
                {t('translate.engineDeepl')}
              </SelectItem>
              <SelectItem value='llm'>{t('translate.engineLLM')}</SelectItem>
            </SelectContent>
          </Select>
          {engine === 'llm' && (
            <Select value={model} onValueChange={setModel}>
              <SelectTrigger className='w-52'>
                <SelectValue placeholder={t('translate.modelPlaceholder')} />
              </SelectTrigger>
              <SelectContent className='max-h-72'>
                {meta.models.map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
      </div>

      <div className='mb-3 flex flex-wrap items-center gap-2'>
        <Select value={sourceLang} onValueChange={setSourceLang}>
          <SelectTrigger className='w-44'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SOURCE_LANGUAGES.map((lang) => (
              <SelectItem key={lang} value={lang}>
                {lang === 'AUTO' ? t('translate.autoDetect') : lang}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size='icon'
          variant='ghost'
          onClick={swap}
          disabled={sourceLang === 'AUTO'}
          title={t('translate.swap')}
        >
          <ArrowLeftRight />
        </Button>
        <Select value={targetLang} onValueChange={setTargetLang}>
          <SelectTrigger className='w-44'>
            <SelectValue placeholder={t('translate.targetPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            {(engine === 'deepl' ? meta.deepl_languages : TARGET_LANGUAGES).map((lang) => (
              <SelectItem key={lang} value={lang}>
                {lang}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {detected && <Badge variant='outline'>{t('translate.detected')} {detected}</Badge>}
      </div>

      <div className='grid gap-4 lg:grid-cols-2'>
        <div className='grid gap-2'>
          <Label htmlFor='translate-source'>{t('translate.sourcePane')}</Label>
          <Textarea
            id='translate-source'
            value={input}
            onChange={(event) => setInput(event.target.value)}
            placeholder={t('translate.sourcePlaceholder')}
            className='min-h-64 resize-y'
          />
        </div>
        <div className='grid gap-2'>
          <div className='flex items-center justify-between'>
            <Label>{t('translate.targetPane')}</Label>
            {output && (
              <Button size='sm' variant='ghost' onClick={copyOutput}>
                {copied ? <Check /> : <Copy />} {copied ? t('translate.copied') : t('translate.copy')}
              </Button>
            )}
          </div>
          <div className='min-h-64 rounded-md border bg-muted/30 p-3 text-sm whitespace-pre-wrap'>
            {running ? (
              <span className='text-muted-foreground inline-flex items-center gap-2'>
                <Loader2 className='h-4 w-4 animate-spin' /> {t('translate.translating')}
              </span>
            ) : (
              output || <span className='text-muted-foreground'>{t('translate.outputEmpty')}</span>
            )}
          </div>
        </div>
      </div>

      <div className='mt-4 flex items-center gap-3'>
        <Button onClick={translate} disabled={!input.trim() || !targetLang || running}>
          {running ? <Loader2 className='animate-spin' /> : <Languages />}
          {t('translate.submit')}
        </Button>
        {activeUsage && <span className='text-muted-foreground text-xs'>{activeUsage}</span>}
        {!deeplAvailable && (
          <span className='text-muted-foreground text-xs'>{t('translate.deeplUnavailable')}</span>
        )}
      </div>

      <Card className='mt-8'>
        <CardHeader>
          <CardTitle className='text-base'>{t('translate.historyTitle')}</CardTitle>
        </CardHeader>
        <CardContent className='grid gap-3'>
          {historyEntries.length === 0 ? (
            <p className='text-muted-foreground text-sm'>{t('translate.historyEmpty')}</p>
          ) : (
            historyEntries.map((record) => (
              <div key={record.id} className='rounded-md border p-3 text-sm'>
                <div className='mb-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground'>
                  <Badge variant='outline'>{record.engine === 'deepl' ? 'DeepL' : record.model}</Badge>
                  <span>
                    {record.detected_lang || record.source_lang || 'AUTO'} → {record.target_lang}
                  </span>
                  <span>{formatUsage(record, t)}</span>
                  {statusBadge(record)}
                </div>
                <p className='text-muted-foreground line-clamp-2'>{record.input_text}</p>
                {record.output_text && (
                  <p className='mt-1 line-clamp-2'>{record.output_text}</p>
                )}
                {record.status === 'failed' && record.error && (
                  <p className='text-destructive mt-1 text-xs'>{record.error}</p>
                )}
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  );
};

// The LLM engine translates into any of these; DeepL restricts the target to
// the languages its channels expose.
const TARGET_LANGUAGES = [
  'ZH', 'EN', 'JA', 'KO', 'DE', 'FR', 'ES', 'PT', 'IT', 'RU', 'AR',
];

export default Translate;
