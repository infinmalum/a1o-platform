import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Download, FileText, Trash2, Upload } from 'lucide-react';

import { ListError, ListLoading, ListPages, studioPageSize, useStudioPage } from '@/components/StudioList';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { knowledgeApi, knowledgeFileUrl, knowledgeSourceUrl, type KnowledgeDocument, type KnowledgeUploadRules } from './knowledgeApi';
import { formatBytes } from './workspaceApi';

export default function KnowledgeLibraries() {
  const { t } = useTranslation();
  const libraryList = useStudioPage(knowledgeApi.listPage);
  const libraries = libraryList.items;
  const [selectedID, setSelected] = useState(0);
  const selected = libraries.find((item) => item.id === selectedID)?.id ?? libraries[0]?.id ?? 0;
  const [docs, setDocs] = useState<{ libraryId: number; items: KnowledgeDocument[]; failed: boolean }>();
  const [rules, setRules] = useState<KnowledgeUploadRules>();
  const [docAttempt, setDocAttempt] = useState(0);
  const [docPage, setDocPage] = useState(0);
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<{ libraryId: number; document?: KnowledgeDocument; name: string }>();
  const uploadRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!selected) return;
    let active = true;
    void Promise.all([knowledgeApi.documents(selected), knowledgeApi.uploadRules()]).then(([items, limits]) => {
      if (!active) return;
      setDocs({ libraryId: selected, items, failed: false });
      setRules(limits);
    }).catch(() => {
      if (active) setDocs({ libraryId: selected, items: [], failed: true });
    });
    return () => { active = false; };
  }, [selected, docAttempt]);

  const perform = async (action: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  const active = libraries.find((item) => item.id === selected);
  const currentDocs = docs?.libraryId === selected ? docs : undefined;
  const documents = currentDocs?.items ?? [];
  const docsReady = !!currentDocs && !currentDocs.failed;
  const usedBytes = documents.reduce((total, document) => total + document.size, 0);
  const updateDocuments = (edit: (items: KnowledgeDocument[]) => KnowledgeDocument[]) => {
    setDocs((current) => current?.libraryId === selected ? { ...current, items: edit(current.items) } : current);
  };

  return (
    <section className='mb-8 grid gap-3 rounded-xl border p-4' aria-labelledby='knowledge-title'>
      <div>
        <h2 id='knowledge-title' className='text-base font-semibold'>{t('knowledge.title')}</h2>
        <p className='text-muted-foreground mt-1 text-sm'>{t('knowledge.description')}</p>
      </div>
      <form className='flex flex-wrap items-end gap-2' onSubmit={(event) => {
        event.preventDefault();
        if (!name.trim() || busy) return;
        void perform(async () => {
          const library = await knowledgeApi.create(name.trim());
          setSelected(library.id);
          setDocPage(0);
          libraryList.setPage(0);
          libraryList.reload();
          setName('');
        });
      }}>
        <div className='grid min-w-0 flex-1 gap-1.5 sm:max-w-xs'>
          <Label htmlFor='knowledge-name'>{t('knowledge.name')}</Label>
          <Input id='knowledge-name' value={name} maxLength={120} disabled={libraryList.loading || busy}
            onChange={(event) => setName(event.target.value)} />
        </div>
        <Button type='submit' disabled={libraryList.loading || busy || !name.trim()}>{t('knowledge.create')}</Button>
      </form>
      {libraryList.loading && <ListLoading />}
      {libraryList.failed && <ListError retry={libraryList.reload} />}
      {!libraryList.loading && !libraryList.failed && !active && (
        <p className='text-muted-foreground text-sm'>{t('knowledge.noLibraries')}</p>
      )}
      {!libraryList.loading && !libraryList.failed && (
        <ListPages page={libraryList.page} more={libraryList.more} onPage={(page) => {
          if (busy) return;
          setSelected(0);
          setDocs(undefined);
          setError('');
          setDocPage(0);
          libraryList.setPage(page);
        }} />
      )}
      {error && <p role='alert' className='text-destructive text-sm'>{error}</p>}
      {active && (
        <div className='grid gap-3 border-t pt-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Label htmlFor='knowledge-library' className='sr-only'>{t('knowledge.title')}</Label>
            <Select value={String(selected)} disabled={busy} onValueChange={(value) => {
              setDocPage(0);
              setDocs(undefined);
              setSelected(Number(value));
              setError('');
            }}>
              <SelectTrigger id='knowledge-library' className='max-w-full sm:w-56'><SelectValue /></SelectTrigger>
              <SelectContent>
                {libraries.map((library) => <SelectItem key={library.id} value={String(library.id)}>{library.name}</SelectItem>)}
              </SelectContent>
            </Select>
            <Input ref={uploadRef} type='file' accept={rules?.extensions.join(',')} className='hidden' onChange={(event) => {
              const file = event.target.files?.[0];
              event.target.value = '';
              if (!file || busy || !docsReady || !rules) return;
              const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0] ?? '';
              if (!rules.extensions.includes(extension)) {
                setError(t('studio.errors.knowledgeInvalidFile'));
                return;
              }
              if (file.size > rules.file_bytes) {
                setError(t('studio.errors.knowledgeFileLimit', { limit: formatBytes(rules.file_bytes) }));
                return;
              }
              if (documents.length >= rules.max_documents || usedBytes + file.size > rules.library_bytes) {
                setError(t('studio.errors.knowledgeCapacity'));
                return;
              }
              void perform(async () => {
                const document = await knowledgeApi.upload(selected, file);
                updateDocuments((list) => [document, ...list]);
                setDocPage(0);
              });
            }} />
            <Button variant='outline' disabled={!docsReady || !rules || busy} onClick={() => uploadRef.current?.click()}>
              <Upload /> {t('knowledge.upload')}
            </Button>
            <Button variant='ghost' disabled={busy} onClick={() => setRemoveTarget({ libraryId: selected, name: active.name })}>
              <Trash2 /> {t('knowledge.deleteLibrary')}
            </Button>
          </div>
          {docsReady && rules && (
            <div className='text-muted-foreground grid gap-1 text-xs'>
              <p>{t('knowledge.limits', {
                file: formatBytes(rules.file_bytes), total: formatBytes(rules.library_bytes),
                used: formatBytes(usedBytes), count: rules.max_documents,
              })}</p>
              <p dir='ltr' className='text-start'>{rules.extensions.join(' · ')}</p>
            </div>
          )}
          {!currentDocs ? <ListLoading /> : currentDocs.failed ? (
            <ListError retry={() => { setDocs(undefined); setDocAttempt((attempt) => attempt + 1); }} />
          ) : documents.length === 0 ? (
            <p className='text-muted-foreground text-sm'>{t('knowledge.empty')}</p>
          ) : (
            <ul className='divide-y rounded-md border'>
              {documents.slice(docPage * studioPageSize, (docPage + 1) * studioPageSize).map((document) => (
                <li key={document.id} className='flex min-w-0 items-center justify-between gap-2 px-3 py-2 text-sm'>
                  <Link to={knowledgeSourceUrl(document.id)} className='flex min-w-0 items-center gap-2 hover:underline'>
                    <FileText className='size-4 shrink-0' /> <span className='truncate'>{document.name}</span>
                  </Link>
                  <div className='flex shrink-0 items-center gap-2'>
                    <span className='text-muted-foreground text-xs'>{formatBytes(document.size)}</span>
                    <Button size='icon-sm' variant='ghost' disabled={busy} aria-label={t('knowledge.deleteNamed', { name: document.name })}
                      onClick={() => setRemoveTarget({ libraryId: selected, name: document.name, document })}>
                      <Trash2 />
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          {docsReady && <ListPages page={docPage} more={documents.length > (docPage + 1) * studioPageSize} onPage={setDocPage} />}
        </div>
      )}
      <Dialog open={!!removeTarget} onOpenChange={(next) => { if (!next && !busy) setRemoveTarget(undefined); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(removeTarget?.document ? 'knowledge.deleteDocument' : 'knowledge.deleteLibrary')}</DialogTitle>
            <DialogDescription>
              {t(removeTarget?.document ? 'knowledge.confirmDocument' : 'knowledge.confirmLibrary', { name: removeTarget?.name })}
            </DialogDescription>
          </DialogHeader>
          {busy && <p className='text-muted-foreground text-sm'>{t('workspace.loading')}</p>}
          <DialogFooter>
            <Button variant='outline' disabled={busy} onClick={() => setRemoveTarget(undefined)}>{t('chat.cancel')}</Button>
            <Button variant='destructive' disabled={busy} onClick={() => {
              if (!removeTarget) return;
              const { libraryId, document } = removeTarget;
              void perform(async () => {
                try {
                  if (document) {
                    await knowledgeApi.deleteDocument(libraryId, document.id);
                    updateDocuments((list) => list.filter((item) => item.id !== document.id));
                    if (documents.length - 1 <= docPage * studioPageSize) setDocPage(Math.max(0, docPage - 1));
                  } else {
                    await knowledgeApi.deleteLibrary(libraryId);
                    setSelected(0);
                    setDocPage(0);
                    setDocs(undefined);
                    libraryList.reload();
                  }
                } finally {
                  setRemoveTarget(undefined);
                }
              });
            }}>{t('chat.delete')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <KnowledgePreview />
    </section>
  );
}

function KnowledgePreview() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const requestedId = Number(params.get('document'));
  const documentId = Number.isSafeInteger(requestedId) && requestedId > 0 ? requestedId : 0;
  const start = Number(params.get('start'));
  const end = Number(params.get('end'));
  const [preview, setPreview] = useState<{ id: number; name?: string; text?: string; contentType?: string; error?: string }>();
  const highlight = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!documentId) return;
    let cancelled = false;
    knowledgeApi.document(documentId).then((data) => {
      if (!cancelled) setPreview({ id: documentId, name: data.document.name, text: data.text, contentType: data.document.content_type });
    }).catch((cause: Error) => {
      if (!cancelled) setPreview({ id: documentId, error: cause.message });
    });
    return () => { cancelled = true; };
  }, [documentId]);

  const current = preview?.id === documentId ? preview : undefined;
  const image = current?.contentType?.startsWith('image/');
  const lines = current?.text?.split('\n');
  const selected = lines && Number.isSafeInteger(start) && Number.isSafeInteger(end) && start > 0 && end >= start && end <= lines.length
    ? lines.slice(start - 1, end).join('\n')
    : undefined;

  useEffect(() => {
    if (selected !== undefined) highlight.current?.scrollIntoView({ block: 'center' });
  }, [selected, start, documentId]);

  return (
    <Dialog open={documentId > 0} onOpenChange={(next) => {
      if (next) return;
      setPreview(undefined);
      setParams((previous) => {
        const updated = new URLSearchParams(previous);
        for (const key of ['document', 'start', 'end']) updated.delete(key);
        return updated;
      }, { replace: true });
    }}>
      <DialogContent className='max-h-[90dvh] overflow-hidden sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle className='break-all'>{current?.name || t('knowledge.source')}</DialogTitle>
          <DialogDescription>
            {selected !== undefined ? t('knowledge.lines', { start, end }) : t(image ? 'knowledge.source' : 'knowledge.original')}
          </DialogDescription>
        </DialogHeader>
        {current?.error ? <p role='alert' className='text-destructive text-sm'>{current.error}</p> : !current ? (
          <p className='text-muted-foreground text-sm'>{t('workspace.loading')}</p>
        ) : image ? (
          <img src={knowledgeFileUrl(documentId, true)} alt={current.name}
            className='max-h-[60dvh] max-w-full rounded-md object-contain' />
        ) : !current.text?.trim() ? (
          <p className='text-muted-foreground text-sm'>{t('knowledge.noText')}</p>
        ) : (
          <pre className='bg-muted max-h-[60dvh] overflow-auto whitespace-pre-wrap break-words rounded-md p-3 text-sm'>
            {lines && selected !== undefined ? (
              <>{start > 1 && lines.slice(0, start - 1).join('\n') + '\n'}<mark ref={highlight}>{selected}</mark>
                {end < lines.length && '\n' + lines.slice(end).join('\n')}</>
            ) : current.text}
          </pre>
        )}
        {current?.name && <DialogFooter>
          <Button variant='outline' asChild>
            <a href={knowledgeFileUrl(documentId)} download={current.name}><Download /> {t('knowledge.download')}</a>
          </Button>
        </DialogFooter>}
      </DialogContent>
    </Dialog>
  );
}
