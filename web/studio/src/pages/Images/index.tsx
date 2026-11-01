import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ImageIcon, Loader2, RefreshCcw, X } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import {
  assetUrl,
  imagesApi,
  type ImageGenStatus,
  type ImageGenerationView,
  type ImageMeta,
} from './imagesApi';

const STATUS_VARIANT: Record<ImageGenStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  queued: 'secondary',
  running: 'default',
  completed: 'outline',
  save_failed: 'destructive',
  failed: 'destructive',
  cancelled: 'outline',
  needs_reconciliation: 'destructive',
};

const DEFAULT_SIZES = ['1024x1024', '1792x1024', '1024x1792', '512x512'];

// Image workbench: prompt → queued persistent run → products. Leaving the
// page never cancels a generation; the list polls until the row settles.
const Images: React.FC = () => {
  const { t } = useTranslation();
  const [meta, setMeta] = useState<ImageMeta>({
    known_models: [],
    other_models: [],
    known_sizes: {},
    max_active: 5,
  });
  const [model, setModel] = useState('');
  const [prompt, setPrompt] = useState('');
  const [size, setSize] = useState('1024x1024');
  const [n, setN] = useState(1);
  const [generations, setGenerations] = useState<ImageGenerationView[]>([]);
  const [gallery, setGallery] = useState<number[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const pollRef = useRef<number | null>(null);

  const modelOptions = [...meta.known_models, ...meta.other_models];
  const sizeOptions = meta.known_sizes[model]?.length ? meta.known_sizes[model] : DEFAULT_SIZES;

  const reload = useCallback(async () => {
    try {
      const data = await imagesApi.list();
      setGenerations(data.generations || []);
      setGallery((data.assets || []).map((asset) => asset.id));
    } catch {
      // toast already surfaced by the client helper
    }
  }, []);

  useEffect(() => {
    imagesApi
      .meta()
      .then((data) => {
        setMeta(data);
        setModel((current) => current || data.known_models[0] || data.other_models[0] || '');
      })
      .catch(() => undefined);
    reload();
  }, [reload]);

  // Poll while any row is queued or running; the worker owns the upstream
  // call, so the page only observes.
  useEffect(() => {
    const active = generations.some(
      (row) => row.status === 'queued' || row.status === 'running'
    );
    if (!active) {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    if (pollRef.current) return;
    pollRef.current = window.setInterval(reload, 3000);
    return () => {
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [generations, reload]);

  const submit = useCallback(async () => {
    if (!model || !prompt.trim() || submitting) return;
    setSubmitting(true);
    try {
      await imagesApi.create({ model, prompt, size, n });
      setPrompt('');
      await reload();
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setSubmitting(false);
    }
  }, [model, prompt, size, n, submitting, reload]);

  const retrySave = async (id: number) => {
    try {
      await imagesApi.retrySave(id);
      await reload();
    } catch {
      // toast already surfaced by the client helper
    }
  };

  const cancel = async (id: number) => {
    try {
      await imagesApi.cancel(id);
      await reload();
    } catch {
      // toast already surfaced by the client helper
    }
  };

  const statusLabel = (status: ImageGenStatus) => t(`images.status.${status}`);
  const activeAssets = (row: ImageGenerationView) => row.assets || [];

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-10'>
      <div className='mb-8'>
        <h1 className='flex items-center gap-2 text-2xl font-semibold tracking-tight'>
          <ImageIcon className='h-6 w-6' /> {t('images.title')}
        </h1>
        <p className='text-muted-foreground mt-1 text-sm'>{t('images.tagline')}</p>
      </div>

      <Card className='mb-8'>
        <CardHeader>
          <CardTitle className='text-base'>{t('images.createTitle')}</CardTitle>
        </CardHeader>
        <CardContent className='grid gap-4'>
          <div className='grid gap-4 sm:grid-cols-3'>
            <div className='grid gap-2'>
              <Label>{t('images.modelLabel')}</Label>
              <Select value={model} onValueChange={setModel}>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('images.modelPlaceholder')} />
                </SelectTrigger>
                <SelectContent className='max-h-72'>
                  {meta.known_models.length > 0 && (
                    <div className='text-muted-foreground px-2 py-1 text-xs'>
                      {t('images.knownModels')}
                    </div>
                  )}
                  {meta.known_models.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                  {meta.other_models.length > 0 && (
                    <div className='text-muted-foreground px-2 pt-2 pb-1 text-xs'>
                      {t('images.otherModels')}
                    </div>
                  )}
                  {meta.other_models.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='grid gap-2'>
              <Label>{t('images.sizeLabel')}</Label>
              <Select value={size} onValueChange={setSize}>
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {sizeOptions.map((option) => (
                    <SelectItem key={option} value={option}>
                      {option}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='grid gap-2'>
              <Label>{t('images.countLabel')}</Label>
              <Select
                value={String(n)}
                onValueChange={(value) => setN(Number(value))}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[1, 2, 3, 4].map((count) => (
                    <SelectItem key={count} value={String(count)}>
                      {count}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='image-prompt'>{t('images.promptLabel')}</Label>
            <Textarea
              id='image-prompt'
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder={t('images.promptPlaceholder')}
              rows={3}
            />
          </div>
          <div>
            <Button onClick={submit} disabled={!model || !prompt.trim() || submitting}>
              {submitting ? <Loader2 className='animate-spin' /> : <ImageIcon />}
              {t('images.submit')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('images.runsTitle')}</h2>
      {generations.length === 0 ? (
        <p className='text-muted-foreground mb-8 text-sm'>{t('images.runsEmpty')}</p>
      ) : (
        <div className='mb-8 grid gap-4'>
          {generations.map((row) => (
            <Card key={row.id}>
              <CardHeader>
                <CardTitle className='flex flex-wrap items-center gap-2 text-sm font-medium'>
                  <Badge variant={STATUS_VARIANT[row.status]}>{statusLabel(row.status)}</Badge>
                  <span>{row.model}</span>
                  <span className='text-muted-foreground'>
                    {row.size} · {row.n}
                  </span>
                  {(row.status === 'queued' || row.status === 'running') && (
                    <Loader2 className='h-3.5 w-3.5 animate-spin' />
                  )}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <p className='text-muted-foreground mb-3 text-sm'>{row.prompt}</p>
                {row.error && <p className='text-destructive mb-2 text-xs'>{row.error}</p>}
                {row.status === 'save_failed' && row.save_error && (
                  <p className='text-destructive mb-2 text-xs'>{row.save_error}</p>
                )}
                <div className='flex flex-wrap items-center gap-3'>
                  {activeAssets(row).map((asset) => (
                    <img
                      key={asset.id}
                      src={assetUrl(asset.id)}
                      alt={row.prompt}
                      className='h-28 w-28 rounded-md border object-cover'
                    />
                  ))}
                  {row.status === 'save_failed' && (
                    <Button size='sm' variant='outline' onClick={() => retrySave(row.id)}>
                      <RefreshCcw /> {t('images.retrySave')}
                    </Button>
                  )}
                  {row.status === 'queued' && (
                    <Button size='sm' variant='ghost' onClick={() => cancel(row.id)}>
                      <X /> {t('images.cancel')}
                    </Button>
                  )}
                  {row.status === 'needs_reconciliation' && (
                    <span className='text-muted-foreground text-xs'>
                      {t('images.needsReconciliation')}
                    </span>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('images.galleryTitle')}</h2>
      {gallery.length === 0 ? (
        <p className='text-muted-foreground text-sm'>{t('images.galleryEmpty')}</p>
      ) : (
        <div className='grid grid-cols-3 gap-3 sm:grid-cols-5 lg:grid-cols-6'>
          {gallery.map((assetId) => (
            <img
              key={assetId}
              src={assetUrl(assetId)}
              alt=''
              className='aspect-square w-full rounded-md border object-cover'
            />
          ))}
        </div>
      )}
    </div>
  );
};

export default Images;
