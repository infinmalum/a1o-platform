import { uploadSize } from '@/helpers/uploadLimits';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { chatApi } from '@/pages/Chat/chatApi';
import { Input } from '@/components/ui/input';
import { useTranslation } from 'react-i18next';
import { Film, ImageIcon, Loader2, RefreshCcw, X } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
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
  type ImageAsset,
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

const GEMINI_VIDEO_DURATIONS = [3, 4, 5, 6, 7, 8];
const ARK_VIDEO_DURATIONS = [5, 6, 7, 8];
const DEFAULT_SIZES = ['1024x1024', '1792x1024', '1024x1792', '512x512'];

// Image workbench: prompt → queued persistent run → products. Leaving the
// page never cancels a generation; the list polls until the row settles.
const Images: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [meta, setMeta] = useState<ImageMeta>({
    known_models: [],
    other_models: [],
    known_sizes: {},
    max_active: 5,
  });
  const [model, setModel] = useState('');
  const [videoMode, setVideoMode] = useState(false);
  const [videoModel, setVideoModel] = useState('');
  const [duration, setDuration] = useState(4);
  const [prompt, setPrompt] = useState('');
  const [size, setSize] = useState('1024x1024');
  const [quality, setQuality] = useState('');
  const [reference, setReference] = useState<{ asset?: ImageAsset; file?: File; fileId?: number; name?: string }>();
  const [referenceError, setReferenceError] = useState('');
  const [attaching, setAttaching] = useState(false);
  const [n, setN] = useState(1);
  const [generations, setGenerations] = useState<ImageGenerationView[]>([]);
  const [gallery, setGallery] = useState<ImageAsset[]>([]);
  const [nextAssets, setNextAssets] = useState(0);
  const [galleryBusy, setGalleryBusy] = useState(false);
  const expandedGallery = useRef(false);
  const [preview, setPreview] = useState<ImageAsset>();
  const [deleting, setDeleting] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ImageAsset>();
  const [pollError, setPollError] = useState(false);
  const promptRef = useRef<HTMLTextAreaElement>(null);
  const [submitting, setSubmitting] = useState(false);


  const referenceLimit = meta.max_reference_bytes ?? 5 * 1024 * 1024;
  const referenceUploadLimit = Math.min(referenceLimit, meta.max_upload_bytes ?? referenceLimit);
  const modelOptions = [...meta.known_models, ...meta.other_models];
  const imageModel = modelOptions.includes(model) ? model : modelOptions[0] || '';
  const activeVideoModel = meta.video_models?.includes(videoModel) ? videoModel : meta.video_models?.[0] || '';
  const arkVideo = meta.ark_video_models?.includes(activeVideoModel) ?? false;
  const videoDurations = arkVideo ? ARK_VIDEO_DURATIONS : GEMINI_VIDEO_DURATIONS;
  const videoDuration = videoDurations.includes(duration) ? duration : videoDurations[0];
  const activeModel = videoMode ? activeVideoModel : imageModel;
  const supportsReference = videoMode ? arkVideo : (meta.reference_models?.includes(imageModel) ?? false);
  const arkImage = meta.ark_image_models?.includes(imageModel) ?? false;
  const qualities = supportsReference && !arkImage && !model.startsWith('gemini-2.5-')
    ? ['1K', '2K', '4K']
    : model === 'dall-e-3' ? ['standard', 'hd'] : [];
  const maxImages = supportsReference || ['dall-e-3', 'step-1x-medium', 'cogview-3'].includes(model) ? 1 : 4;
  const sizeOptions = meta.known_sizes[model]?.length ? meta.known_sizes[model] : DEFAULT_SIZES;

  const reload = useCallback(async () => {
    try {
      const data = await imagesApi.list();
      setGenerations(data.generations || []);
      const latest = data.assets || [];
      const visible = latest.slice(0, 24);
      const cutoff = visible[visible.length - 1]?.id ?? 0;
      setGallery((current) => expandedGallery.current
        ? [...visible, ...current.filter((asset) => asset.id < cutoff)]
        : visible);
      if (!expandedGallery.current) setNextAssets(latest.length > 24 ? cutoff : 0);
    } catch {
      // toast already surfaced by the client helper
    }
  }, []);

  useEffect(() => {
    imagesApi
      .meta()
      .then((data) => {
        setMeta(data);
        const defaultModel = data.known_models[0] || data.other_models[0] || '';
        setModel(defaultModel);
        const sizes = data.known_sizes[defaultModel] || DEFAULT_SIZES;
        setSize((current) => sizes.includes(current) ? current : sizes[0]);
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const activeRunIds = generations.filter(({ status }) => status === 'queued' || status === 'running')
    .map(({ id }) => id).join(',');
  useEffect(() => {
    if (!activeRunIds) {
      setPollError(false);
      return;
    }
    const controller = new AbortController();
    const ids = activeRunIds.split(',').map(Number);
    let timer: number;
    let failures = 0;
    const poll = async () => {
      const results = await Promise.allSettled(ids.map((id) => imagesApi.get(id, controller.signal)));
      if (controller.signal.aborted) return;
      const updates = new Map<number, ImageGenerationView>();
      for (const result of results) {
        if (result.status === 'fulfilled') updates.set(result.value.id, result.value);
      }
      const failed = updates.size < ids.length;
      setPollError(failed);
      failures = failed ? failures + 1 : 0;
      if (updates.size) {
        setGenerations((current) => current.map((row) => {
          const latest = updates.get(row.id);
          return latest && (row.status === 'queued' || row.status === 'running') ? latest : row;
        }));
        if ([...updates.values()].some((update) => update.status === 'completed')) void reload();
      }
      if (ids.some((id) => {
        const status = updates.get(id)?.status;
        return !status || status === 'queued' || status === 'running';
      })) {
        timer = window.setTimeout(poll, failed ? Math.min(30000, 3000 * 2 ** failures) : 3000);
      }
    };
    timer = window.setTimeout(poll, 3000);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [activeRunIds, reload]);

  const submit = useCallback(async () => {
    if (!activeModel || !prompt.trim() || submitting || (reference && !supportsReference)) return;
    setSubmitting(true);
    try {
      let fileId = reference?.fileId;
      if (reference?.file) {
        const stored = await chatApi.uploadFile(reference.file);
        fileId = stored.id;
        setReference((current) => current === reference ? { fileId: stored.id, name: stored.name } : current);
      }
      await imagesApi.create({
        model: activeModel, prompt, size, n: videoMode ? 1 : n,
        quality: videoMode ? (arkVideo ? '720p' : '360p') : quality || undefined,
        video_duration: videoMode ? videoDuration : undefined,
        reference_asset_id: reference?.asset?.id, reference_file_id: fileId,
      });
      setPrompt('');
      await reload();
    } catch {
      // toast already surfaced by the client helper
    } finally {
      setSubmitting(false);
    }
  }, [activeModel, prompt, size, n, quality, reference, submitting, supportsReference, videoMode, videoDuration, arkVideo, reload]);

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

  const restore = (row: ImageGenerationView) => {
    if (row.video_duration) {
      setVideoMode(true);
      setVideoModel(row.model);
      setReference(undefined);
      setDuration(row.video_duration);
      setSize(row.size);
    } else {
      setVideoMode(false);
      const availableSizes = meta.known_sizes[row.model] || DEFAULT_SIZES;
      setModel(row.model);
      setSize(availableSizes.includes(row.size) ? row.size : availableSizes[0]);
      setN(row.n);
      setQuality(row.quality || '');
      setReference(undefined);
    }
    setPrompt(row.prompt);
    promptRef.current?.focus();
  };

  const thumbnail = (asset: ImageAsset, prompt?: string) => (
    <button
      key={asset.id}
      type='button'
      onClick={() => setPreview(asset)}
      aria-label={t(asset.mime === 'video/mp4' ? 'images.openVideo' : 'images.openImage', { id: asset.id })}
      className='block w-full overflow-hidden rounded-md border bg-muted focus-visible:outline-2'
    >
      {asset.mime === 'video/mp4' ? (
        <span className='flex aspect-square items-center justify-center' aria-hidden='true'><Film className='size-7' /></span>
      ) : (
        <img src={assetUrl(asset.id) + '?thumbnail=1'} alt={prompt || ''} loading='lazy' decoding='async'
          className='aspect-square w-full object-contain' />
      )}
    </button>
  );

  const useReference = () => {
    const referenceModel = !videoMode && supportsReference ? model : meta.reference_models?.[0];
    const useInVideo = videoMode && supportsReference;
    if (!preview || (!useInVideo && !referenceModel)) return;
    setVideoMode(useInVideo);
    if (preview.size > referenceLimit || !['image/jpeg', 'image/png', 'image/webp'].includes(preview.mime)) {
      setReferenceError(t('images.referenceFileLimit', { limit: uploadSize(referenceLimit) }));
      setPreview(undefined);
      return;
    }
    if (!useInVideo && referenceModel && (referenceModel !== model || videoMode)) {
      setModel(referenceModel);
      setSize(meta.known_sizes[referenceModel]?.[0] || '1:1');
      setQuality('');
    }
    setN(1);
    setReference({ asset: preview });
    setReferenceError('');
    setPreview(undefined);
    setTimeout(() => promptRef.current?.focus(), 0);
  };

  const attachPreview = async (destination: 'chat' | 'work') => {
    if (!preview || attaching) return;
    setAttaching(true);
    try {
      const file = await imagesApi.attachAsset(preview.id);
      navigate(`/${destination}?attachment=${file.id}`);
    } catch {
      // The API helper surfaces the error.
    } finally {
      setAttaching(false);
    }
  };

  const deletePreview = async () => {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await imagesApi.deleteAsset(deleteTarget.id);
      setPreview(undefined);
      setDeleteTarget(undefined);
      setGallery((current) => current.filter((asset) => asset.id !== deleteTarget.id));
      await reload();
    } catch {
      // The API helper shows the error and the preview stays open.
    } finally {
      setDeleting(false);
    }
  };

  const loadMoreAssets = async () => {
    if (!nextAssets || galleryBusy) return;
    setGalleryBusy(true);
    try {
      const page = await imagesApi.assetPage(nextAssets);
      setGallery((current) => {
        const known = new Set(current.map((asset) => asset.id));
        return [...current, ...(page.assets || []).filter((asset) => !known.has(asset.id))];
      });
      expandedGallery.current = true;
      setNextAssets(page.next);
    } catch {
      // The API helper surfaces the error.
    } finally {
      setGalleryBusy(false);
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
        <Button variant='outline' size='sm' className='mt-3' asChild><Link to='/audio'>{t('audio.title')}</Link></Button>
      </div>

      <Card className='mb-8'>
        <CardHeader>
          <CardTitle className='text-base'>{t(videoMode ? 'images.videoTitle' : 'images.createTitle')}</CardTitle>
          {!!meta.video_models?.length && (
            <div className='flex gap-2'>
              <Button size='sm' variant={videoMode ? 'outline' : 'default'} aria-pressed={!videoMode}
                disabled={submitting} onClick={() => {
                  setVideoMode(false);
                  setReference(undefined);
                  setReferenceError('');
                  setSize(meta.known_sizes[imageModel]?.[0] || DEFAULT_SIZES[0]);
                }}>{t('images.imageMode')}</Button>
              <Button size='sm' variant={videoMode ? 'default' : 'outline'} aria-pressed={videoMode}
                disabled={submitting} onClick={() => {
                  setVideoMode(true);
                  setReference(undefined);
                  setReferenceError('');
                  setSize('16:9');
                }}>{t('images.videoMode')}</Button>
            </div>
          )}
        </CardHeader>
        <CardContent className='grid gap-4'>
          {videoMode ? (
            <>
              <p className='text-muted-foreground text-sm'>
                {arkVideo ? '720p · MP4' : t('images.videoLimits', { model: activeModel })}
              </p>
              <div className='grid gap-4 sm:grid-cols-3'>
                <div className='grid gap-2'>
                  <Label htmlFor='video-model'>{t('images.modelLabel')}</Label>
                  <Select value={activeVideoModel} onValueChange={(next) => {
                    setVideoModel(next);
                    setReference(undefined);
                    setReferenceError('');
                  }}>
                    <SelectTrigger id='video-model'><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {meta.video_models?.map((name) => <SelectItem key={name} value={name}>{name}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='video-ratio'>{t('images.aspectRatio')}</Label>
                  <Select value={size} onValueChange={setSize}>
                    <SelectTrigger id='video-ratio'><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value='16:9'>16:9</SelectItem>
                      <SelectItem value='9:16'>9:16</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='video-duration'>{t('images.duration')}</Label>
                  <Select value={String(videoDuration)} onValueChange={(value) => setDuration(Number(value))}>
                    <SelectTrigger id='video-duration'><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {videoDurations.map((seconds) => (
                        <SelectItem key={seconds} value={String(seconds)}>{t('images.seconds', { seconds })}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </>
          ) : (
            <div className='grid gap-4 sm:grid-cols-3'>
              <div className='grid gap-2'>
                <Label>{t('images.modelLabel')}</Label>
                <Select value={model} onValueChange={(next) => {
                  setModel(next);
                  setQuality('');
                  setN(1);
                  const allowed = meta.known_sizes[next]?.length ? meta.known_sizes[next] : DEFAULT_SIZES;
                  setSize((current) => allowed.includes(current) ? current : allowed[0]);
                }}>
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
                <Label>{t(supportsReference && !arkImage ? 'images.aspectRatio' : 'images.sizeLabel')}</Label>
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
                    {[1, 2, 3, 4].slice(0, maxImages).map((count) => (
                      <SelectItem key={count} value={String(count)}>
                        {count}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          )}
          {!videoMode && qualities.length > 0 && (
            <div className='grid max-w-xs gap-2'>
              <Label htmlFor='image-quality'>{t(supportsReference ? 'images.resolution' : 'images.quality')}</Label>
              <Select value={quality || 'default'} onValueChange={(value) => setQuality(value === 'default' ? '' : value)}>
                <SelectTrigger id='image-quality'><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value='default'>{t('images.default')}</SelectItem>
                  {qualities.map((value) => <SelectItem key={value} value={value}>{value}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          )}
          {(supportsReference || reference || referenceError) && (
            <div className='grid gap-2'>
              {supportsReference && (
                <>
                  <Label htmlFor='image-reference'>{t(arkVideo && videoMode ? 'images.firstFrame' : 'images.reference')}</Label>
                  <Input id='image-reference' type='file' accept='image/png,image/jpeg,image/webp'
                    disabled={submitting} onChange={(event) => {
                      const file = event.target.files?.[0];
                      event.target.value = '';
                      if (!file) return;
                      if (file.size > referenceUploadLimit || !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
                        setReferenceError(t('images.referenceFileLimit', { limit: uploadSize(referenceUploadLimit) }));
                        return;
                      }
                      setReferenceError('');
                      setReference({ file });
                    }} />
                </>
              )}
              {reference && (
                <div className='flex flex-wrap items-center gap-2 text-sm'>
                  {reference.asset && <img src={assetUrl(reference.asset.id) + '?thumbnail=1'} alt='' className='size-12 object-contain' />}
                  <span>{reference.file?.name || reference.name || t('images.openImage', { id: reference.asset?.id })}</span>
                  <Button size='sm' variant='ghost' disabled={submitting} onClick={() => setReference(undefined)}>
                    {t('images.removeReference')}
                  </Button>
                  {!supportsReference && <span role='alert'>{t('images.selectReferenceModel')}</span>}
                </div>
              )}
              {referenceError && <p role='alert' className='text-destructive text-sm'>{referenceError}</p>}
            </div>
          )}
          <div className='grid gap-2'>
            <Label htmlFor='image-prompt'>{t('images.promptLabel')}</Label>
            <Textarea
              id='image-prompt'
              ref={promptRef}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder={t('images.promptPlaceholder')}
              rows={3}
            />
          </div>
          <div>
            <Button onClick={submit} disabled={!activeModel || !prompt.trim() || submitting || (!!reference && !supportsReference)}>
              {submitting ? <Loader2 className='animate-spin' /> : <ImageIcon />}
              {t('images.submit')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <h2 className='mb-3 text-sm font-medium text-muted-foreground'>{t('images.runsTitle')}</h2>
      {pollError && <p role='status' className='text-muted-foreground mb-3 text-sm'>{t('images.pollDelayed')}</p>}
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
                    {row.size} · {row.video_duration ? t('images.seconds', { seconds: row.video_duration }) : row.n}
                  </span>
                  {(row.status === 'queued' || row.status === 'running') && (
                    <Loader2 className='h-3.5 w-3.5 animate-spin' />
                  )}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <p className='text-muted-foreground mb-3 text-sm'>{row.prompt}</p>
                {(row.status === 'queued' || row.status === 'running') && (
                  <p className='text-muted-foreground mb-2 text-xs' role='status'>
                    {t(row.status === 'queued' ? 'images.queuedHint' : 'images.runningHint')}
                  </p>
                )}
                {row.status === 'failed' && <p className='text-destructive mb-2 text-xs'>{t('images.generationFailed')}</p>}
                {row.status === 'save_failed' && <p className='text-destructive mb-2 text-xs'>{t('images.saveFailedHint')}</p>}
                <div className='flex flex-wrap items-center gap-3'>
                  {activeAssets(row).map((asset) => (
                    <div key={asset.id} className='w-28'>{thumbnail(asset, row.prompt)}</div>
                  ))}
                  {(row.video_duration ? meta.video_models?.includes(row.model) : modelOptions.includes(row.model)) && (
                    <Button size='sm' variant='outline' onClick={() => restore(row)}>
                      {t('images.reuseSettings')}
                    </Button>
                  )}
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
          {gallery.map((asset) => thumbnail(asset))}
        </div>
      )}
      {nextAssets > 0 && (
        <Button className='mt-4' variant='outline' onClick={loadMoreAssets} disabled={galleryBusy}>
          {t('images.loadMore')}
        </Button>
      )}
      <Dialog open={!!preview} onOpenChange={(open) => { if (!open) setPreview(undefined); }}>
        <DialogContent className='max-h-[95dvh] overflow-y-auto sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t(preview?.mime === 'video/mp4' ? 'images.videoPreview' : 'images.previewTitle')}</DialogTitle>
            <DialogDescription>
              {generations.find((row) => row.id === preview?.run_id)?.prompt || t(preview?.mime === 'video/mp4' ? 'images.videoPreview' : 'images.previewDescription')}
            </DialogDescription>
          </DialogHeader>
          {preview && (
            <>
              {preview.mime === 'video/mp4' ? (
                <video src={assetUrl(preview.id)} controls preload='metadata' className='max-h-[65dvh] w-full' />
              ) : (
                <img src={assetUrl(preview.id)} alt={t('images.openImage', { id: preview.id })}
                  className='max-h-[65dvh] w-full object-contain' />
              )}
              <div className='flex flex-wrap gap-2'>
                <Button asChild>
                  <a href={assetUrl(preview.id)} download={preview.mime === "video/mp4" ? `video-${preview.id}.mp4` : `image-${preview.id}.${imageExtension(preview.mime)}`}>
                    {t('images.download')}
                  </a>
                </Button>
                {preview.mime !== 'video/mp4' && (
                  <>
                    {(supportsReference || !!meta.reference_models?.length) && (
                      <Button variant='outline' onClick={useReference}>{t('images.continueEdit')}</Button>
                    )}
                    <Button variant='outline' disabled={attaching || deleting} onClick={() => attachPreview('chat')}>
                      {t('images.useInChat')}
                    </Button>
                    <Button variant='outline' disabled={attaching || deleting} onClick={() => attachPreview('work')}>
                      {t('images.useInWork')}
                    </Button>
                  </>
                )}
                <Button variant='destructive' onClick={() => setDeleteTarget(preview)} disabled={deleting}>
                  {t(preview.mime === 'video/mp4' ? 'images.deleteVideo' : 'images.delete')}
                </Button>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={!!deleteTarget} onOpenChange={(open) => { if (!open && !deleting) setDeleteTarget(undefined); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(deleteTarget?.mime === 'video/mp4' ? 'images.deleteVideo' : 'images.delete')}</DialogTitle>
            <DialogDescription>{t(deleteTarget?.mime === 'video/mp4' ? 'images.confirmDeleteVideo' : 'images.confirmDelete')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant='outline' disabled={deleting} onClick={() => setDeleteTarget(undefined)}>{t('chat.cancel')}</Button>
            <Button variant='destructive' disabled={deleting} onClick={() => void deletePreview()}>{t('studio.list.confirm')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

function imageExtension(mime: string) {
  const subtype = mime?.split('/')[1];
  return ['png', 'jpeg', 'webp', 'gif', 'avif'].includes(subtype) ? subtype : 'img';
}

export default Images;
